package events

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/segmentio/kafka-go"
)

const (
	AssetChangedTopic = "cmdb.asset.changed.v1"
	AssetStatusTopic  = "cmdb.asset.status.v1"
)

type AssetEventHandler func(context.Context, string, []byte) error

type consumerReader interface {
	FetchMessage(context.Context) (kafka.Message, error)
	CommitMessages(context.Context, ...kafka.Message) error
	Close() error
}

type AssetEventConsumer struct {
	readers   []consumerReader
	handler   AssetEventHandler
	status    atomic.Value
	processed atomic.Uint64
	lastEvent atomic.Int64
}

func NewAssetEventConsumer(brokers string, handler AssetEventHandler) (*AssetEventConsumer, error) {
	if strings.TrimSpace(brokers) == "" {
		return nil, errors.New("KAFKA_BROKERS is required")
	}
	if handler == nil {
		return nil, errors.New("asset event handler is required")
	}
	instance := strings.TrimSpace(os.Getenv("POD_NAME"))
	if instance == "" {
		instance, _ = os.Hostname()
	}
	groupSuffix := eventConsumerGroupSuffix(instance)
	topics := []string{AssetChangedTopic, AssetStatusTopic}
	readers := make([]consumerReader, 0, len(topics))
	for _, topic := range topics {
		readers = append(readers, kafka.NewReader(kafka.ReaderConfig{
			Brokers:     strings.Split(brokers, ","),
			Topic:       topic,
			GroupID:     "cmdb-asset-refresh-v1-" + groupSuffix + "-" + strings.ReplaceAll(topic, ".", "-"),
			StartOffset: kafka.LastOffset,
			MinBytes:    1,
			MaxBytes:    10e6,
			MaxWait:     time.Second,
		}))
	}
	consumer := &AssetEventConsumer{readers: readers, handler: handler}
	consumer.status.Store("stopped")
	return consumer, nil
}

func eventConsumerGroupSuffix(instance string) string {
	sum := sha1.Sum([]byte(instance))
	return hex.EncodeToString(sum[:8])
}

func (c *AssetEventConsumer) Status() string {
	if c == nil {
		return "disabled"
	}
	status, _ := c.status.Load().(string)
	if status == "" {
		return "stopped"
	}
	return status
}

func (c *AssetEventConsumer) Running() bool {
	return c != nil && c.Status() == "running"
}

func (c *AssetEventConsumer) Processed() uint64 {
	if c == nil {
		return 0
	}
	return c.processed.Load()
}

func (c *AssetEventConsumer) LastEventAt() string {
	if c == nil {
		return ""
	}
	value := c.lastEvent.Load()
	if value == 0 {
		return ""
	}
	return time.Unix(value, 0).Local().Format("2006-01-02 15:04:05")
}

func (c *AssetEventConsumer) Run(ctx context.Context) {
	if c == nil {
		return
	}
	c.status.Store("running")
	defer c.status.Store("stopped")
	var wait sync.WaitGroup
	for _, reader := range c.readers {
		wait.Add(1)
		go func(reader consumerReader) {
			defer wait.Done()
			defer reader.Close()
			c.consume(ctx, reader)
		}(reader)
	}
	wait.Wait()
}

func (c *AssetEventConsumer) consume(ctx context.Context, reader consumerReader) {
	for {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("fetch CMDB asset event", "error", err)
			if !waitForContext(ctx, time.Second) {
				return
			}
			continue
		}
		var handlerErr error
		for attempt := 1; attempt <= 3; attempt++ {
			if handlerErr = c.handler(ctx, message.Topic, message.Value); handlerErr == nil {
				break
			}
			if attempt < 3 && !waitForContext(ctx, time.Duration(attempt)*time.Second) {
				return
			}
		}
		if handlerErr != nil {
			slog.Error("handle CMDB asset event", "topic", message.Topic, "key", string(message.Key), "error", handlerErr)
		} else {
			c.processed.Add(1)
			c.lastEvent.Store(time.Now().Unix())
		}
		if err = reader.CommitMessages(ctx, message); err != nil && ctx.Err() == nil {
			slog.Error("commit CMDB asset event", "topic", message.Topic, "error", err)
		}
	}
}

func waitForContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *AssetEventConsumer) String() string {
	return fmt.Sprintf("asset event consumer (%s)", c.Status())
}
