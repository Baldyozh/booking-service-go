package worker

import (
	"context"
	"time"

	"go.uber.org/zap"

	"booking-service/app/messaging"
	"booking-service/app/models"
)

// StuckCancellationWorker повторно отправляет команды отмены для зависших бронирований.
//
// Логика работы:
//  1. Найти бронирования в cancellation_pending, у которых cancel_command_sent_at старше таймаута
//  2. Повторно опубликовать CancelBookingJobCommand в Catalog
//  3. Не менять статус в БД — полагаться на обычный flow (успех или DLQ rollback)
type StuckCancellationWorker struct {
	repo      models.BookingRepository
	publisher *messaging.Publisher
	interval  time.Duration
	timeout   time.Duration
	batchSize int
	logger    *zap.Logger
}

// NewStuckCancellationWorker создаёт новый воркер обработки зависших отмен.
func NewStuckCancellationWorker(
	repo models.BookingRepository,
	publisher *messaging.Publisher,
	interval, timeout time.Duration,
	batchSize int,
	logger *zap.Logger,
) *StuckCancellationWorker {
	return &StuckCancellationWorker{
		repo:      repo,
		publisher: publisher,
		interval:  interval,
		timeout:   timeout,
		batchSize: batchSize,
		logger:    logger,
	}
}

// Run запускает воркер. Блокирует до отмены контекста.
func (w *StuckCancellationWorker) Run(ctx context.Context) {
	w.logger.Info("воркер зависших отмен запущен",
		zap.Duration("interval", w.interval),
		zap.Duration("timeout", w.timeout),
		zap.Int("batchSize", w.batchSize),
	)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("воркер зависших отмен остановлен")
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *StuckCancellationWorker) processBatch(ctx context.Context) {
	sentBefore := time.Now().Add(-w.timeout)

	bookings, err := w.repo.GetStuckCancellations(ctx, sentBefore, w.batchSize)
	if err != nil {
		w.logger.Error("ошибка получения зависших отмен", zap.Error(err))
		return
	}

	if len(bookings) == 0 {
		return
	}

	w.logger.Info("повторная отправка команд отмены", zap.Int("count", len(bookings)))

	for _, booking := range bookings {
		w.processBooking(ctx, &booking)
	}
}

func (w *StuckCancellationWorker) processBooking(ctx context.Context, booking *models.Booking) {
	bookingID := booking.ID()
	logger := w.logger.With(
		zap.Int64("bookingId", bookingID),
		zap.Time("cancelCommandSentAt", booking.CancelCommandSentAt()),
	)

	if err := w.publisher.PublishCancelBookingJob(ctx, messaging.CancelBookingJobCommand{
		EventId:   messaging.NewMessageID(),
		RequestId: messaging.BookingIDToRequestID(bookingID),
	}); err != nil {
		logger.Error("ошибка повторной публикации CancelBookingJob", zap.Error(err))
		return
	}

	logger.Info("команда отмены повторно отправлена в Catalog")
}
