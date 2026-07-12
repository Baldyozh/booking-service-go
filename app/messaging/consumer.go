package messaging

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// MessageHandler -- функция обработки сообщения.
type MessageHandler func(ctx context.Context, body []byte) error

// subscription хранит routing key и обработчик для конкретной очереди.
type subscription struct {
	routingKey string
	handler    MessageHandler
}

// Consumer потребляет сообщения из RabbitMQ.
type Consumer struct {
	conn          *Connection
	exchangeName  string
	queuePrefix   string
	subscriptions map[string]subscription // queueSuffix → subscription
	logger        *zap.Logger
}

// NewConsumer создаёт нового потребителя.
func NewConsumer(conn *Connection, exchangeName, queuePrefix string, logger *zap.Logger) *Consumer {
	return &Consumer{
		conn:          conn,
		exchangeName:  exchangeName,
		queuePrefix:   queuePrefix,
		subscriptions: make(map[string]subscription),
		logger:        logger,
	}
}

// Subscribe регистрирует обработчик для очереди.
// queueSuffix — читаемый суффикс имени очереди (напр. "booking-job.confirmed").
// routingKey — routing key для биндинга к exchange (может быть длинным Rebus-именем типа).
func (c *Consumer) Subscribe(queueSuffix, routingKey string, handler MessageHandler) {
	c.subscriptions[queueSuffix] = subscription{routingKey: routingKey, handler: handler}
}

// Start запускает потребление сообщений. Блокирует до отмены контекста
func (c *Consumer) Start(ctx context.Context) error {
	// Объявление exchange
	if err := c.conn.DeclareExchange(c.exchangeName); err != nil {
		return fmt.Errorf("объявление exchange: %w", err)
	}

	// Для каждой подписки создаём очередь и запускаем горутину потребления
	for queueSuffix, sub := range c.subscriptions {
		queueName := fmt.Sprintf("%s.%s", c.queuePrefix, queueSuffix)

		_, err := c.conn.DeclareAndBindQueue(queueName, c.exchangeName, sub.routingKey)
		if err != nil {
			return fmt.Errorf("настройка очереди %s: %w", queueName, err)
		}

		deliveries, err := c.conn.Channel().Consume(
			queueName, // queue
			"",        // consumer tag (auto-generated)
			false,     // auto-ack (false = manual ack)
			false,     // exclusive
			false,     // no-local
			false,     // no-wait
			nil,       // arguments
		)
		if err != nil {
			return fmt.Errorf("начало потребления из %s: %w", queueName, err)
		}

		go c.consume(ctx, queueName, sub.routingKey, deliveries, sub.handler)

		c.logger.Info("подписка на события настроена",
			zap.String("queue", queueName),
			zap.String("routingKey", sub.routingKey),
		)
	}

	return nil
}

// consume обрабатывает сообщения из канала deliveries.
func (c *Consumer) consume(
	ctx context.Context,
	queueName, routingKey string,
	deliveries <-chan amqp.Delivery,
	handler MessageHandler,
) {
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("потребление остановлено", zap.String("queue", queueName))
			return

		case delivery, ok := <-deliveries:
			if !ok {
				c.logger.Warn("канал доставки закрыт", zap.String("queue", queueName))
				return
			}

			c.processDelivery(ctx, routingKey, delivery, handler)
		}
	}
}

// processDelivery обрабатывает одно сообщение.
func (c *Consumer) processDelivery(
	ctx context.Context,
	routingKey string,
	delivery amqp.Delivery,
	handler MessageHandler,
) {
	logger := c.logger.With(
		zap.String("routingKey", routingKey),
		zap.Uint64("deliveryTag", delivery.DeliveryTag),
	)

	logger.Debug("получено сообщение", zap.String("body", string(delivery.Body)))

	if err := handler(ctx, delivery.Body); err != nil {
		logger.Error("ошибка обработки сообщения", zap.Error(err))
		// Nack без requeue -- сообщение попадёт в dead letter queue (если настроена)
		_ = delivery.Nack(false, false)
		return
	}

	// Ack -- сообщение обработано успешно
	if err := delivery.Ack(false); err != nil {
		logger.Error("ошибка подтверждения сообщения", zap.Error(err))
	}

	logger.Debug("сообщение обработано успешно")
}
