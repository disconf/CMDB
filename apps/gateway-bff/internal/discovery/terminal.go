package discovery

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/ssh"
)

type TerminalTicket struct {
	Ticket    string `json:"ticket"`
	ExpiresAt string `json:"expiresAt"`
}

type terminalTicketRecord struct {
	SessionID string
	Operator  string
	Roles     []string
	ExpiresAt time.Time
}

type RemoteTerminalEvent struct {
	Sequence  int64  `json:"sequence"`
	Direction string `json:"direction"`
	Data      string `json:"data"`
	CreatedAt string `json:"createdAt"`
}

type TerminalRecording struct {
	Events    []RemoteTerminalEvent `json:"events"`
	Truncated bool                  `json:"truncated"`
}

type RemoteTerminalEndpoint interface {
	Subscribe(access string) (uint64, bool)
	Output(subscriber uint64) (<-chan []byte, bool)
	Unsubscribe(subscriber uint64) int
	Write(data []byte) (int, error)
	Resize(cols, rows int) error
	Presence() (int, bool)
	Close() error
	IsOwner() bool
}

type RemoteTerminalChannel struct {
	client           *ssh.Client
	session          *ssh.Session
	stdin            io.WriteCloser
	stdout           io.Reader
	service          *Service
	sessionID        string
	mu               sync.Mutex
	subscribers      map[uint64]chan []byte
	subscriberAccess map[uint64]string
	nextSubscriber   uint64
	closed           bool
	pumpOnce         sync.Once
	closeOnce        sync.Once
	subscribersOnce  sync.Once
	closeErr         error
	busCancel        context.CancelFunc
	leaseCancel      context.CancelFunc
}

type RemoteTerminalProxy struct {
	service            *Service
	sessionID          string
	access             string
	mu                 sync.Mutex
	subscribers        map[uint64]chan []byte
	subscriberAccess   map[uint64]string
	nextSubscriber     uint64
	closed             bool
	readerOnce         sync.Once
	subscribersOnce    sync.Once
	closeOnce          sync.Once
	cancel             context.CancelFunc
	outputSub          *redis.PubSub
	participantCancels map[uint64]context.CancelFunc
}

var blockedTerminalPatterns = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	{regexp.MustCompile(`(?i)(^|[;&|]\s*)(sudo\s+)?rm\s+(-[a-z]*r[a-z]*f?|-[a-z]*f[a-z]*r)\s+(/\s*$|/\*\s*$|/(etc|usr|var|boot|bin|sbin|lib|lib64|root)\b)`), "禁止递归强制删除系统目录"},
	{regexp.MustCompile(`(?i)\bmkfs(\.[a-z0-9]+)?\b`), "禁止格式化文件系统"},
	{regexp.MustCompile(`(?i)\b(fdisk|parted|sgdisk)\b`), "禁止修改磁盘分区"},
	{regexp.MustCompile(`(?i)\bdd\b[^\n]*\bof=/dev/(sd|nvme|vd|hd|mapper)`), "禁止直接覆写块设备"},
	{regexp.MustCompile(`(?i)(^|[;&|]\s*)(sudo\s+)?(shutdown|reboot|poweroff|halt|init\s+0)\b`), "禁止关闭或重启主机"},
	{regexp.MustCompile(`(?i)(^|[;&|]\s*)(sudo\s+)?kill\s+-9\s+1\b`), "禁止终止 init/systemd 主进程"},
	{regexp.MustCompile(`(?i)(^|[;&|]\s*)(sudo\s+)?chmod\s+-R\s+777\s+/\s*$`), "禁止递归开放根目录权限"},
	{regexp.MustCompile(`(?i):\(\)\s*\{\s*:\|:&\s*\};:`), "禁止执行 Fork Bomb"},
}

func (s *Service) CreateTerminalTicket(sessionID, operator string, roles []string) (TerminalTicket, error) {
	item, _, err := s.sessionForOperator(sessionID, operator, roles)
	if err != nil {
		return TerminalTicket{}, err
	}
	_ = item
	data := make([]byte, 32)
	if _, err = rand.Read(data); err != nil {
		return TerminalTicket{}, err
	}
	expires := time.Now().Add(60 * time.Second)
	ticket := hex.EncodeToString(data)
	s.mu.Lock()
	for key, record := range s.terminalTickets {
		if record.ExpiresAt.Before(time.Now()) {
			delete(s.terminalTickets, key)
		}
	}
	s.terminalTickets[ticket] = terminalTicketRecord{SessionID: item.ID, Operator: operator, Roles: append([]string(nil), roles...), ExpiresAt: expires}
	s.mu.Unlock()
	return TerminalTicket{Ticket: ticket, ExpiresAt: expires.Format("2006-01-02 15:04:05")}, nil
}

func (s *Service) ConsumeTerminalTicket(ticket string) (RemoteSession, error) {
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return RemoteSession{}, ErrRemoteValidation
	}
	s.mu.Lock()
	record, ok := s.terminalTickets[ticket]
	if ok {
		delete(s.terminalTickets, ticket)
	}
	s.mu.Unlock()
	if !ok || record.ExpiresAt.Before(time.Now()) {
		return RemoteSession{}, ErrNotFound
	}
	item, _, err := s.sessionForOperator(record.SessionID, record.Operator, record.Roles)
	if err != nil {
		return RemoteSession{}, err
	}
	return item, nil
}

func (s *Service) JoinRemoteTerminal(sessionID, operator string, roles []string, cols, rows int) (*RemoteTerminalChannel, uint64, string, error) {
	item, _, err := s.sessionForOperator(sessionID, operator, roles)
	if err != nil {
		return nil, 0, "", err
	}
	access := item.AccessMode
	cols, rows = normalizeTerminalSize(cols, rows)
	channel, err := s.ensureLocalRemoteTerminal(item, cols, rows)
	if err != nil {
		return nil, 0, "", err
	}
	subscriber, ok := channel.Subscribe(access)
	if !ok {
		return nil, 0, "", ErrRemoteValidation
	}
	return channel, subscriber, access, nil
}

func (s *Service) OpenRemoteTerminal(sessionID, operator string, roles []string, cols, rows int) (RemoteTerminalEndpoint, uint64, string, error) {
	item, _, err := s.sessionForOperator(sessionID, operator, roles)
	if err != nil {
		return nil, 0, "", err
	}
	access := item.AccessMode
	cols, rows = normalizeTerminalSize(cols, rows)

	s.mu.Lock()
	channel := s.terminals[sessionID]
	s.mu.Unlock()
	if channel != nil {
		subscriber, ok := channel.Subscribe(access)
		if !ok {
			return nil, 0, "", ErrRemoteValidation
		}
		return channel, subscriber, access, nil
	}

	if s.terminalBus == nil {
		if access != "control" {
			return nil, 0, "", ErrRemoteForbidden
		}
		channel, err = s.ensureLocalRemoteTerminal(item, cols, rows)
		if err != nil {
			return nil, 0, "", err
		}
		subscriber, ok := channel.Subscribe(access)
		if !ok {
			return nil, 0, "", ErrRemoteValidation
		}
		return channel, subscriber, access, nil
	}

	owner, err := s.AcquireRemoteSessionLease(context.Background(), sessionID)
	if err == nil && owner == s.instanceID {
		channel, err = s.ensureLocalRemoteTerminal(item, cols, rows)
		if err != nil {
			return nil, 0, "", err
		}
		subscriber, ok := channel.Subscribe(access)
		if !ok {
			return nil, 0, "", ErrRemoteValidation
		}
		return channel, subscriber, access, nil
	}
	if err != nil && !errors.Is(err, ErrRemoteForbidden) {
		return nil, 0, "", err
	}

	proxy, err := newRemoteTerminalProxy(s, item, access)
	if err != nil {
		return nil, 0, "", err
	}
	subscriber, ok := proxy.Subscribe(access)
	if !ok {
		_ = proxy.Close()
		return nil, 0, "", ErrRemoteValidation
	}
	return proxy, subscriber, access, nil
}

func (s *Service) ensureLocalRemoteTerminal(item RemoteSession, cols, rows int) (*RemoteTerminalChannel, error) {
	s.mu.Lock()
	existing := s.terminals[item.ID]
	s.mu.Unlock()
	if existing != nil {
		return existing, nil
	}
	channel, err := s.newRemoteTerminalChannel(item, cols, rows)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if current := s.terminals[item.ID]; current != nil {
		s.mu.Unlock()
		_ = channel.Close()
		return current, nil
	}
	s.terminals[item.ID] = channel
	s.mu.Unlock()
	channel.startPump()
	return channel, nil
}

func normalizeTerminalSize(cols, rows int) (int, int) {
	if cols < 40 {
		cols = 120
	}
	if rows < 12 {
		rows = 32
	}
	if cols > 400 {
		cols = 400
	}
	if rows > 200 {
		rows = 200
	}
	return cols, rows
}

func (s *Service) newRemoteTerminalChannel(item RemoteSession, cols, rows int) (*RemoteTerminalChannel, error) {
	username, secret, err := s.resolveRemoteCredential(item.CredentialID)
	if err != nil {
		return nil, err
	}
	client, err := s.dialTrusted(item.AssetID, item.IP, item.Port, username, secret)
	if err != nil {
		return nil, err
	}
	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err = session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}
	if err = session.Shell(); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}
	channel := &RemoteTerminalChannel{client: client, session: session, stdin: stdin, stdout: stdout, service: s, sessionID: item.ID, subscribers: map[uint64]chan []byte{}, subscriberAccess: map[uint64]string{}}
	if s.terminalBus != nil {
		channel.startControlConsumer()
		channel.startLeaseLoop()
	}
	return channel, nil
}

func (c *RemoteTerminalChannel) Read(p []byte) (int, error) {
	return c.stdout.Read(p)
}

func (c *RemoteTerminalChannel) Write(p []byte) (int, error) {
	return c.stdin.Write(p)
}

func (c *RemoteTerminalChannel) Resize(cols, rows int) error {
	if cols < 40 || rows < 12 || cols > 400 || rows > 200 {
		return ErrRemoteValidation
	}
	return c.session.WindowChange(rows, cols)
}

func (c *RemoteTerminalChannel) IsOwner() bool { return true }

func (c *RemoteTerminalChannel) startControlConsumer() {
	bus := c.service.terminalBus
	if bus == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.busCancel = cancel
	go func() {
		for ctx.Err() == nil {
			subscribeCtx, stopSubscribe := context.WithTimeout(ctx, remoteTerminalControlWait)
			subscription, err := bus.SubscribeControl(subscribeCtx, c.sessionID)
			stopSubscribe()
			if err != nil {
				bus.logError("subscribe control", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
					continue
				}
			}
			messages := subscription.Channel()
			reconnect := false
			for !reconnect && ctx.Err() == nil {
				select {
				case <-ctx.Done():
					_ = subscription.Close()
					return
				case payload, ok := <-messages:
					if !ok {
						reconnect = true
						break
					}
					event, decodeErr := decodeRemoteTerminalBusEvent(payload.Payload)
					if decodeErr != nil {
						bus.logError("decode control", decodeErr)
						continue
					}
					switch event.Kind {
					case "input":
						data, dataErr := remoteTerminalEventData(event)
						if dataErr != nil {
							bus.logError("decode input", dataErr)
							continue
						}
						if _, err = c.Write(data); err != nil {
							bus.logError("write proxied input", err)
						}
					case "resize":
						if err = c.Resize(event.Cols, event.Rows); err != nil {
							bus.logError("resize proxied terminal", err)
						}
					case "close":
						_ = subscription.Close()
						_ = c.Close()
						return
					case "release":
						time.AfterFunc(time.Second, func() {
							if c.service != nil {
								c.service.MaybeCloseRemoteTerminal(c.sessionID)
							}
						})
					}
				}
			}
			_ = subscription.Close()
			if ctx.Err() == nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
			}
		}
	}()
}

func (c *RemoteTerminalChannel) startLeaseLoop() {
	if c.service == nil || c.service.db == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.leaseCancel = cancel
	interval := c.service.RemoteSessionLeaseRenewalInterval()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := c.service.RenewRemoteSessionLease(ctx, c.sessionID); err != nil {
					c.service.terminalBus.logError("renew owner lease", err)
					_ = c.Close()
					return
				}
			}
		}
	}()
}

func (c *RemoteTerminalChannel) Subscribe(access string) (uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, false
	}
	c.nextSubscriber++
	subscriber := c.nextSubscriber
	c.subscribers[subscriber] = make(chan []byte, 512)
	c.subscriberAccess[subscriber] = access
	return subscriber, true
}

func (c *RemoteTerminalChannel) Output(subscriber uint64) (<-chan []byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	output, ok := c.subscribers[subscriber]
	return output, ok
}

func (c *RemoteTerminalChannel) Unsubscribe(subscriber uint64) int {
	c.mu.Lock()
	output, ok := c.subscribers[subscriber]
	if ok {
		delete(c.subscribers, subscriber)
		delete(c.subscriberAccess, subscriber)
		close(output)
	}
	remaining := len(c.subscribers)
	c.mu.Unlock()
	return remaining
}

func (c *RemoteTerminalChannel) Presence() (int, bool) {
	c.mu.Lock()
	count := len(c.subscribers)
	controllerOnline := false
	for _, access := range c.subscriberAccess {
		if access == "control" {
			controllerOnline = true
			break
		}
	}
	c.mu.Unlock()
	if c.service != nil && c.service.terminalBus != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		remoteCount, remoteController := c.service.terminalBus.Presence(ctx, c.sessionID)
		cancel()
		count += remoteCount
		controllerOnline = controllerOnline || remoteController
	}
	return count, controllerOnline
}

func (c *RemoteTerminalChannel) startPump() {
	c.pumpOnce.Do(func() {
		go func() {
			defer c.Close()
			buffer := make([]byte, 8192)
			for {
				count, readErr := c.stdout.Read(buffer)
				if count > 0 {
					chunk := append([]byte(nil), buffer[:count]...)
					c.service.recordRemoteTerminalOutput(c.sessionID, chunk)
					c.broadcast(chunk)
				}
				if readErr != nil {
					return
				}
			}
		}()
	})
}

func (c *RemoteTerminalChannel) broadcast(data []byte) {
	chunk := append([]byte(nil), data...)
	c.mu.Lock()
	defer c.mu.Unlock()
	for subscriber, output := range c.subscribers {
		select {
		case output <- chunk:
		default:
			delete(c.subscribers, subscriber)
			delete(c.subscriberAccess, subscriber)
			close(output)
		}
	}
}

func (c *RemoteTerminalChannel) closeSubscribers() {
	c.subscribersOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		for subscriber, output := range c.subscribers {
			delete(c.subscribers, subscriber)
			delete(c.subscriberAccess, subscriber)
			close(output)
		}
		c.mu.Unlock()
	})
}

func (c *RemoteTerminalChannel) Close() error {
	c.closeOnce.Do(func() {
		if c.busCancel != nil {
			c.busCancel()
		}
		if c.leaseCancel != nil {
			c.leaseCancel()
		}
		if c.service != nil && c.service.terminalBus != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			c.service.terminalBus.PublishOwnerOffline(ctx, c.sessionID)
			cancel()
		}
		_ = c.stdin.Close()
		_ = c.session.Close()
		c.closeErr = c.client.Close()
		if c.service != nil {
			c.service.mu.Lock()
			if c.service.terminals[c.sessionID] == c {
				delete(c.service.terminals, c.sessionID)
			}
			c.service.mu.Unlock()
		}
		c.closeSubscribers()
	})
	return c.closeErr
}

func (s *Service) CloseRemoteTerminal(sessionID string) {
	s.mu.RLock()
	channel := s.terminals[sessionID]
	s.mu.RUnlock()
	if channel != nil {
		_ = channel.Close()
		return
	}
	if s.terminalBus == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s.terminalBus.PublishClose(ctx, sessionID)
}

func (s *Service) MaybeCloseRemoteTerminal(sessionID string) {
	s.mu.RLock()
	channel := s.terminals[sessionID]
	s.mu.RUnlock()
	if channel == nil {
		return
	}
	count, _ := channel.Presence()
	if count == 0 {
		_ = channel.Close()
	}
}

func (s *Service) WriteRemoteTerminalInput(sessionID string, data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	s.mu.RLock()
	channel := s.terminals[sessionID]
	s.mu.RUnlock()
	if channel != nil {
		return channel.Write(data)
	}
	if s.terminalBus == nil {
		return 0, ErrRemoteValidation
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.terminalBus.PublishInput(ctx, sessionID, "", data); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (s *Service) recordRemoteTerminalOutput(sessionID string, data []byte) {
	if len(data) == 0 {
		return
	}
	sequence, err := s.AppendTerminalEventWithSequence(sessionID, "output", string(data))
	if err != nil {
		if s.terminalBus != nil {
			s.terminalBus.logError("record output", err)
		}
		return
	}
	if s.terminalBus == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = s.terminalBus.PublishOutput(ctx, sessionID, int64(sequence), data); err != nil {
		s.terminalBus.logError("publish output", err)
	}
}

func (s *Service) AppendTerminalEvent(sessionID, direction, data string) error {
	_, err := s.AppendTerminalEventWithSequence(sessionID, direction, data)
	return err
}

func (s *Service) AppendTerminalEventWithSequence(sessionID, direction, data string) (uint64, error) {
	if strings.TrimSpace(sessionID) == "" || data == "" || len(data) > 256<<10 {
		return 0, nil
	}
	sequence, err := s.nextRemoteTerminalSequence(sessionID)
	if err != nil {
		return 0, err
	}
	if s.db == nil {
		return sequence, nil
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(data))
	_, err = s.db.Exec(`INSERT INTO remote_terminal_events(session_id,sequence,direction,data) VALUES($1,$2,$3,$4) ON CONFLICT(session_id,sequence) DO NOTHING`, sessionID, sequence, direction, encoded)
	if err != nil {
		return 0, err
	}
	return sequence, nil
}

func (s *Service) nextRemoteTerminalSequence(sessionID string) (uint64, error) {
	if s.terminalBus != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		sequence, err := s.terminalBus.NextSequence(ctx, sessionID)
		cancel()
		if err == nil && sequence > 0 {
			return uint64(sequence), nil
		}
		if err != nil {
			s.terminalBus.logError("next sequence", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminalSequences == nil {
		s.terminalSequences = map[string]uint64{}
	}
	s.terminalSequences[sessionID]++
	return s.terminalSequences[sessionID], nil
}

func newRemoteTerminalProxy(service *Service, item RemoteSession, access string) (*RemoteTerminalProxy, error) {
	if service == nil || service.terminalBus == nil {
		return nil, ErrRemoteValidation
	}
	ctx, cancel := context.WithCancel(context.Background())
	subscribeCtx, stopSubscribe := context.WithTimeout(ctx, remoteTerminalControlWait)
	subscription, err := service.terminalBus.SubscribeOutput(subscribeCtx, item.ID)
	stopSubscribe()
	if err != nil {
		cancel()
		return nil, err
	}
	proxy := &RemoteTerminalProxy{
		service:            service,
		sessionID:          item.ID,
		access:             access,
		subscribers:        map[uint64]chan []byte{},
		subscriberAccess:   map[uint64]string{},
		participantCancels: map[uint64]context.CancelFunc{},
		cancel:             cancel,
		outputSub:          subscription,
	}
	go proxy.readLoop(ctx)
	return proxy, nil
}

func (p *RemoteTerminalProxy) readLoop(ctx context.Context) {
	if p.outputSub == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case payload, ok := <-p.outputSub.Channel():
			if !ok {
				p.closeSubscribers()
				return
			}
			event, err := decodeRemoteTerminalBusEvent(payload.Payload)
			if err != nil {
				p.service.terminalBus.logError("decode output", err)
				continue
			}
			switch event.Kind {
			case "output":
				data, dataErr := remoteTerminalEventData(event)
				if dataErr != nil {
					p.service.terminalBus.logError("decode output data", dataErr)
					continue
				}
				p.broadcast(data)
			case "owner_offline":
				p.closeSubscribers()
				return
			}
		}
	}
}

func (p *RemoteTerminalProxy) IsOwner() bool { return false }

func (p *RemoteTerminalProxy) Subscribe(access string) (uint64, bool) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return 0, false
	}
	p.nextSubscriber++
	subscriber := p.nextSubscriber
	p.subscribers[subscriber] = make(chan []byte, 512)
	p.subscriberAccess[subscriber] = access
	p.mu.Unlock()
	p.startParticipantHeartbeat(subscriber, access)
	return subscriber, true
}

func (p *RemoteTerminalProxy) startParticipantHeartbeat(subscriber uint64, access string) {
	heartbeatCtx, cancel := context.WithCancel(context.Background())
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		cancel()
		return
	}
	p.participantCancels[subscriber] = cancel
	p.mu.Unlock()
	participantID := p.service.instanceID + ":" + access + ":" + strconv.FormatUint(subscriber, 10)
	send := func() {
		ctx, stop := context.WithTimeout(heartbeatCtx, time.Second)
		defer stop()
		if err := p.service.terminalBus.HeartbeatPresence(ctx, p.sessionID, participantID, access); err != nil {
			p.service.terminalBus.logError("heartbeat presence", err)
		}
	}
	send()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				ctx, stop := context.WithTimeout(context.Background(), time.Second)
				_ = p.service.terminalBus.RemovePresence(ctx, p.sessionID, participantID)
				stop()
				return
			case <-ticker.C:
				send()
			}
		}
	}()
}

func (p *RemoteTerminalProxy) Output(subscriber uint64) (<-chan []byte, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	output, ok := p.subscribers[subscriber]
	return output, ok
}

func (p *RemoteTerminalProxy) Unsubscribe(subscriber uint64) int {
	p.mu.Lock()
	output, ok := p.subscribers[subscriber]
	if ok {
		delete(p.subscribers, subscriber)
		delete(p.subscriberAccess, subscriber)
		close(output)
	}
	cancel := p.participantCancels[subscriber]
	delete(p.participantCancels, subscriber)
	remaining := len(p.subscribers)
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return remaining
}

func (p *RemoteTerminalProxy) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.service.terminalBus.PublishInput(ctx, p.sessionID, "", data); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (p *RemoteTerminalProxy) Resize(cols, rows int) error {
	if cols < 40 || rows < 12 || cols > 400 || rows > 200 {
		return ErrRemoteValidation
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return p.service.terminalBus.PublishResize(ctx, p.sessionID, cols, rows)
}

func (p *RemoteTerminalProxy) Presence() (int, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	return p.service.terminalBus.Presence(ctx, p.sessionID)
}

func (p *RemoteTerminalProxy) broadcast(data []byte) {
	chunk := append([]byte(nil), data...)
	p.mu.Lock()
	defer p.mu.Unlock()
	for subscriber, output := range p.subscribers {
		select {
		case output <- chunk:
		default:
			delete(p.subscribers, subscriber)
			delete(p.subscriberAccess, subscriber)
			if cancel := p.participantCancels[subscriber]; cancel != nil {
				delete(p.participantCancels, subscriber)
				cancel()
			}
			close(output)
		}
	}
}

func (p *RemoteTerminalProxy) closeSubscribers() {
	p.subscribersOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		for subscriber, output := range p.subscribers {
			delete(p.subscribers, subscriber)
			delete(p.subscriberAccess, subscriber)
			if cancel := p.participantCancels[subscriber]; cancel != nil {
				delete(p.participantCancels, subscriber)
				cancel()
			}
			close(output)
		}
		p.mu.Unlock()
	})
}

func (p *RemoteTerminalProxy) Close() error {
	p.closeOnce.Do(func() {
		if p.cancel != nil {
			p.cancel()
		}
		if p.outputSub != nil {
			_ = p.outputSub.Close()
		}
		p.closeSubscribers()
		if p.service != nil && p.service.terminalBus != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			if err := p.service.terminalBus.PublishRelease(ctx, p.sessionID); err != nil {
				p.service.terminalBus.logError("publish release", err)
			}
			cancel()
		}
	})
	return nil
}

func (s *Service) TerminalRecording(sessionID, username string, roles []string) (TerminalRecording, error) {
	item, err := s.RemoteSessionReplay(sessionID, username, roles)
	if err != nil {
		return TerminalRecording{}, err
	}
	result := TerminalRecording{Events: []RemoteTerminalEvent{}}
	if s.db == nil {
		return s.terminalRecordingFromArchive(item)
	}
	rows, err := s.db.Query(`SELECT sequence,direction,data,created_at FROM remote_terminal_events WHERE session_id=$1 ORDER BY sequence LIMIT 20001`, sessionID)
	if err != nil {
		return TerminalRecording{}, err
	}
	for rows.Next() {
		var event RemoteTerminalEvent
		var encoded string
		var created time.Time
		if err = rows.Scan(&event.Sequence, &event.Direction, &encoded, &created); err != nil {
			_ = rows.Close()
			return TerminalRecording{}, err
		}
		event.Data = encoded
		event.CreatedAt = created.Local().Format("2006-01-02 15:04:05.000")
		if len(result.Events) >= 20000 {
			result.Truncated = true
			break
		}
		result.Events = append(result.Events, event)
	}
	if err = rows.Close(); err != nil {
		return TerminalRecording{}, err
	}
	if len(result.Events) > 0 || item.ArchiveKey == "" || s.objectStore == nil {
		return result, nil
	}
	return s.terminalRecordingFromArchive(item)
}

func (s *Service) terminalRecordingFromArchive(item RemoteSession) (TerminalRecording, error) {
	if item.ArchiveStatus == "expired" {
		return TerminalRecording{Events: []RemoteTerminalEvent{}}, nil
	}
	if item.ArchiveKey == "" || s.objectStore == nil {
		return TerminalRecording{Events: []RemoteTerminalEvent{}}, nil
	}
	content, err := s.objectStore.Get(context.Background(), item.ArchiveKey)
	if err != nil {
		return TerminalRecording{}, err
	}
	return decodeRecordingArchive(content)
}

func (s *Service) RecordRemoteSessionEvent(sessionID, level, kind, message string) {
	_, _ = s.appendSessionLog(sessionID, level, kind, message, 0)
}

func BlockedTerminalCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	for _, item := range blockedTerminalPatterns {
		if item.pattern.MatchString(command) {
			return item.reason
		}
	}
	return ""
}
