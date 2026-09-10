package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

type resourceEvent struct {
	Data struct {
		TaskID, ID, Name, IP, Type string
		Confidence                 int
	} `json:"data"`
}

func (s *Service) Consume(ctx context.Context, brokers string) {
	s.consumeGroup(ctx, brokers, "cmdb-discovery-staging-v1")
}

func (s *Service) consumeGroup(ctx context.Context, brokers, groupID string) {
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: strings.Split(brokers, ","), Topic: "discovery.resource.found.v1", GroupID: groupID, StartOffset: kafka.FirstOffset, MinBytes: 1, MaxBytes: 10e6, MaxWait: time.Second})
	defer reader.Close()
	for {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("fetch discovery event", "error", err)
			}
			return
		}
		if err = s.consumeMessage(message.Value); err != nil {
			slog.Error("stage discovered resource", "error", err)
			continue
		}
		if err = reader.CommitMessages(ctx, message); err != nil {
			slog.Error("commit discovery event", "error", err)
		}
	}
}

func (s *Service) consumeMessage(payload []byte) error {
	var event resourceEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}
	d := event.Data
	if d.TaskID == "" {
		d.TaskID = "kafka-" + time.Now().Format("20060102")
	}
	if d.ID == "" {
		d.ID = "found-" + fmt.Sprint(time.Now().UnixNano())
	}
	if d.Type == "" {
		d.Type = "physical-server"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := -1
	for i := range s.tasks {
		if s.tasks[i].ID == d.TaskID {
			index = i
			break
		}
	}
	if index < 0 {
		task := Task{ID: d.TaskID, Name: "Kafka 自动发现", Source: "kafka", Scope: "事件总线", Status: "completed", CreatedAt: time.Now().Format("2006-01-02 15:04")}
		if err := s.persistTask(task); err != nil {
			return err
		}
		s.tasks = append(s.tasks, task)
		index = len(s.tasks) - 1
	}
	item := DiscoveredItem{ID: d.ID, Name: d.Name, IP: d.IP, Type: d.Type, Confidence: d.Confidence, State: "pending", Result: "pending", Message: "等待入库", UpdatedAt: time.Now().Format("2006-01-02 15:04:05")}
	if err := s.persistItem(d.TaskID, item, payload); err != nil {
		return err
	}
	for i := range s.tasks[index].Items {
		if s.tasks[index].Items[i].ID == item.ID {
			s.tasks[index].Items[i] = item
			return nil
		}
	}
	s.tasks[index].Items = append(s.tasks[index].Items, item)
	s.tasks[index].Discovered = len(s.tasks[index].Items)
	return nil
}
