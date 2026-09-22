// Package rabbitmq provides a simple publisher for RabbitMQ using the AMQP 0.9.1
// protocol. It handles connection management, exchange/queue declaration, and
// message publishing.
package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/rabbitmq/amqp091-go"
)

// Message represents a CROID event published to RabbitMQ.
type Message struct {
	Croid     string `json:"croid"`
	CroType   string `json:"cro_type"`
	CroValue  string `json:"cro_value"`
	System    string `json:"system"`
	CreatedAt string `json:"created_at"`
	// Record is the full JSON record associated with this CROID (optional, may
	// be empty when the record is fetched from the source system instead).
	Record string `json:"record,omitempty"`
}

// Publisher manages a RabbitMQ connection and channel for publishing CROID
// events to an exchange.
type Publisher struct {
	conn         *amqp091.Connection
	channel      *amqp091.Channel
	exchange     string
	log          *slog.Logger
}

// New creates a Publisher connected to the RabbitMQ server at url, declaring
// the named exchange as a direct exchange. Returns an error if the connection
// or exchange declaration fails.
//
// If logger is nil, a default logger is used.
func New(url, exchange string, logger *slog.Logger) (*Publisher, error) {
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

	err = ch.ExchangeDeclare(
		exchange,
		"direct",
		true,  // durable
		false, // auto-deleted
		false, // internal
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("rabbitmq: exchange declare %s: %w", exchange, err)
	}

	p := &Publisher{
		conn:     conn,
		channel:  ch,
		exchange: exchange,
		log:      logger,
	}

	// Watch for channel errors (e.g., queue not found, channel closed by server)
	go func() {
		err := <-ch.NotifyClose(make(chan *amqp091.Error))
		if err != nil {
			p.log.Error("rabbitmq: channel closed", "error", err)
		}
	}()

	// Watch for connection errors
	go func() {
		err := <-conn.NotifyClose(make(chan *amqp091.Error))
		if err != nil {
			p.log.Error("rabbitmq: connection closed", "error", err)
		}
	}()

	logger.Info("rabbitmq: connected", "url", url, "exchange", exchange)
	return p, nil
}

// Publish sends a CROID message to the exchange. If the exchange does not
// exist on the server, Publish returns nil (amqp091 treats undeclared
// exchanges as a no-op on publish when the exchange is not mandatory).
func (p *Publisher) Publish(msg Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("rabbitmq: marshal: %w", err)
	}

	err = p.channel.PublishWithContext(
		context.Background(),
		p.exchange, // exchange
		"",         // routing key (not used for direct exchange without binding)
		false,      // mandatory
		false,      // immediate
		amqp091.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
	if err != nil {
		p.log.Error("rabbitmq: publish", "error", err)
		return fmt.Errorf("rabbitmq: publish: %w", err)
	}
	p.log.Info("rabbitmq: published", "croid", msg.Croid, "system", msg.System)
	return nil
}

// Close releases the RabbitMQ channel and connection.
func (p *Publisher) Close() error {
	if p.channel != nil {
		if err := p.channel.Close(); err != nil {
			p.log.Error("rabbitmq: close channel", "error", err)
		}
	}
	if p.conn != nil {
		if err := p.conn.Close(); err != nil {
			p.log.Error("rabbitmq: close connection", "error", err)
		}
	}
	p.log.Info("rabbitmq: disconnected")
	return nil
}
