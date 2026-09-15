package events

import (
	"context"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

type fakeConsumerReader struct {
	messages  []kafka.Message
	committed int
	closed    bool
}

func (f *fakeConsumerReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	if len(f.messages) > 0 {
		message := f.messages[0]
		f.messages = f.messages[1:]
		return message, nil
	}
	<-ctx.Done()
	return kafka.Message{}, ctx.Err()
}

func (f *fakeConsumerReader) CommitMessages(context.Context, ...kafka.Message) error {
	f.committed++
	return nil
}

func (f *fakeConsumerReader) Close() error {
	f.closed = true
	return nil
}

func TestAssetEventConsumerProcessesAndCommits(t *testing.T) {
	reader := &fakeConsumerReader{messages: []kafka.Message{{Topic: AssetChangedTopic, Key: []byte("asset-1"), Value: []byte(`{"eventType":"cmdb.asset.changed.v1"}`)}}}
	consumer := &AssetEventConsumer{readers: []consumerReader{reader}}
	consumer.status.Store("stopped")
	consumer.handler = func(context.Context, string, []byte) error { return nil }
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	consumer.consume(ctx, reader)
	if reader.committed != 1 {
		t.Fatalf("expected one committed event, got %d", reader.committed)
	}
	if consumer.Processed() != 1 {
		t.Fatalf("expected one processed event, got %d", consumer.Processed())
	}
	if consumer.LastEventAt() == "" {
		t.Fatal("expected last event timestamp")
	}
}

func TestNewAssetEventConsumerRequiresConfiguration(t *testing.T) {
	if _, err := NewAssetEventConsumer("", nil); err == nil {
		t.Fatal("expected missing broker configuration to fail")
	}
}
