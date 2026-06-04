// Package consumer subscribes to NATS JetStream status events.
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2/event"
	"github.com/dcm-project/control-plane/internal/sp/store"
	rmstore "github.com/dcm-project/control-plane/internal/sp/store/resource_manager"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// StatusEvent represents a status event payload.
type StatusEvent struct {
	Id        string    `json:"id"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// StatusConsumer subscribes to status events from NATS JetStream
// and updates ServiceTypeInstance records in the database.
type StatusConsumer struct {
	conn         *nats.Conn
	js           jetstream.JetStream
	consumeCtx   jetstream.ConsumeContext
	store        store.Store
	subject      string
	streamName   string
	consumerName string
}

// New creates a new StatusConsumer connected to the given NATS URL.
func New(natsURL, subject string, st store.Store, opts ...Option) (*StatusConsumer, error) {
	conn, err := nats.Connect(natsURL,
		nats.MaxReconnects(-1),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			slog.Warn("NATS disconnected", "error", err)
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			slog.Info("NATS reconnected")
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	sc := &StatusConsumer{
		conn:         conn,
		js:           js,
		store:        st,
		subject:      subject,
		streamName:   "dcm-status",
		consumerName: "service-provider-manager",
	}
	for _, o := range opts {
		o(sc)
	}
	return sc, nil
}

// Option configures a StatusConsumer.
type Option func(*StatusConsumer)

// SetStreamName sets the JetStream stream name.
func SetStreamName(name string) Option {
	return func(c *StatusConsumer) { c.streamName = name }
}

// SetConsumerName sets the JetStream durable consumer name.
func SetConsumerName(name string) Option {
	return func(c *StatusConsumer) { c.consumerName = name }
}

// Start creates the JetStream stream and consumer, then begins processing messages.
func (c *StatusConsumer) Start(ctx context.Context) error {
	stream, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     c.streamName,
		Subjects: []string{c.subject},
	})
	if err != nil {
		return fmt.Errorf("failed to create/update stream %s: %w", c.streamName, err)
	}

	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:   c.consumerName,
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return fmt.Errorf("failed to create/update consumer %s: %w", c.consumerName, err)
	}

	cc, err := cons.Consume(func(msg jetstream.Msg) {
		c.handleMessage(ctx, msg)
	})
	if err != nil {
		return fmt.Errorf("failed to start consuming: %w", err)
	}
	c.consumeCtx = cc

	slog.Info("StatusConsumer subscribed",
		"subject", c.subject,
		"stream", c.streamName,
		"consumer", c.consumerName,
	)
	return nil
}

// Stop stops the consumer and closes the NATS connection.
func (c *StatusConsumer) Stop() {
	if c.consumeCtx != nil {
		c.consumeCtx.Stop()
	}
	c.conn.Close()
	slog.Info("StatusConsumer stopped")
}

func (c *StatusConsumer) handleMessage(ctx context.Context, msg jetstream.Msg) {
	var event cloudevents.Event
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		slog.Error("Failed to parse CloudEvent", "error", err)
		_ = msg.Ack()
		return
	}

	var payload StatusEvent
	if err := json.Unmarshal(event.Data(), &payload); err != nil {
		slog.Error("Failed to deserialize event payload", "error", err)
		_ = msg.Ack()
		return
	}

	if payload.Id == "" {
		slog.Warn("Event missing instance ID, discarding")
		_ = msg.Ack()
		return
	}

	if err := c.store.ServiceTypeInstance().UpdateStatus(ctx, payload.Id, payload.Status, payload.Message); err != nil {
		if errors.Is(err, rmstore.ErrInstanceNotFound) {
			slog.Warn("No instance found, skipping status update", "instance_id", payload.Id)
			_ = msg.Ack()
			return
		}
		slog.Error("Failed to update instance status", "instance_id", payload.Id, "error", err)
		_ = msg.Nak()
		return
	}

	slog.Info("Instance status updated", "instance_id", payload.Id, "status", payload.Status)
	_ = msg.Ack()
}
