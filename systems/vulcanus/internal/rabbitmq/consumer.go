// Package rabbitmq provides a consumer for RabbitMQ that delivers CROID events
// to a handler function. It manages connection/channel setup, queue binding,
// and message acknowledgment.
package rabbitmq

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/rabbitmq/amqp091-go"
)

// Record is a CROID event received from RabbitMQ.
type Record struct {
	Croid     string `json:"croid"`
	CroType   string `json:"cro_type"`
	CroValue  string `json:"cro_value"`
	System    string `json:"system"`
	CreatedAt string `json:"created_at"`
	// Record contains the full JSON record (may be empty).
	Record string `json:"record,omitempty"`
}

// Handler processes a single CROID record. Return an error to trigger NACK
// and requeue (or send to dead-letter if a bound DLX exists).
type Handler func(Record) error

// Consumer manages a RabbitMQ connection, channel, queue, and deliveries.
type Consumer struct {
	conn  *amqp091.Connection
	ch    *amqp091.Channel
	log   *slog.Logger
	handler Handler
	wg    sync.WaitGroup
	done  chan struct{}
}

// New creates a Consumer connected to the RabbitMQ server at url, declares
// a unique consumer queue bound to the named exchange with the given routing
// key, and starts dispatching messages to handler. Returns an error if the
// connection or queue declaration fails.
//
// If logger is nil, a default logger is used.
func New(url, exchange, queue, routingKey string, handler Handler, logger *slog.Logger) (*Consumer, error) {
	if logger == nil {
		logger = slog.Default()
	}

	conn, err := amqp091.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: dial %s: %w", url, err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("rabbitmq: channel: %w", err)
	}

	err = ch.Qos(
		1,     // prefetch count: process one message at a time
		0,     // prefetch size: no limit
		false, // global
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("rabbitmq: qos: %w", err)
	}

	_, err = ch.QueueDeclare(
		queue,
		true,  // durable
		false, // auto-deleted
		false, // exclusive
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("rabbitmq: queue declare %s: %w", queue, err)
	}

	err = ch.QueueBind(
		queue,
		routingKey,
		exchange,
		false,
		nil,
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("rabbitmq: queue bind %s: %w", queue, err)
	}

	deliveries, err := ch.Consume(
		queue,
		"", // consumer tag
		false, // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("rabbitmq: consume: %w", err)
	}

	c := &Consumer{
		conn:    conn,
		ch:      ch,
		log:     logger,
		handler: handler,
		done:    make(chan struct{}),
	}

	c.wg.Add(1)
	go c.loop(deliveries)

	logger.Info("rabbitmq: connected", "url", url, "exchange", exchange, "queue", queue)
	return c, nil
}

// Done returns a channel that is closed when the consumer loop exits.
func (c *Consumer) Done() <-chan struct{} {
	return c.done
}

// loop reads from deliveries and dispatches to the handler.
func (c *Consumer) loop(deliveries <-chan amqp091.Delivery) {
	defer close(c.done)
	defer c.wg.Done()
	for d := range deliveries {
		rec := Record{}
		if err := json.Unmarshal(d.Body, &rec); err != nil {
			c.log.Error("rabbitmq: unmarshal record", "err", err)
			_ = d.Nack(false, false)
			continue
		}

		c.log.Debug("rabbitmq: processing message", "croid", rec.Croid, "system", rec.System)

		if err := c.handler(rec); err != nil {
			c.log.Error("rabbitmq: handler", "err", err)
			_ = d.Nack(false, false)
			continue
		}

		_ = d.Ack(false)
	}
}

// Close releases the RabbitMQ channel and connection, waiting for the
// consumer loop to exit.
func (c *Consumer) Close() error {
	var err error
	if c.ch != nil {
		if e := c.ch.Close(); e != nil {
			err = e
		}
	}
	if c.conn != nil {
		if e := c.conn.Close(); e != nil && err == nil {
			err = e
		}
	}
	c.wg.Wait()
	if err != nil {
		c.log.Error("rabbitmq: close", "err", err)
	} else {
		c.log.Info("rabbitmq: disconnected")
	}
	return err
}
