package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"booking-service/app/messaging"
	"booking-service/app/service"
)

// CancelBookingErrorHandler обрабатывает события CancelBookingJobError (DLQ).
type CancelBookingErrorHandler struct {
	service *service.BookingsService
	logger  *zap.Logger
}

// NewCancelBookingErrorHandler создаёт новый обработчик.
func NewCancelBookingErrorHandler(svc *service.BookingsService, logger *zap.Logger) *CancelBookingErrorHandler {
	return &CancelBookingErrorHandler{
		service: svc,
		logger:  logger,
	}
}

// Handle обрабатывает событие ошибки отмены — выполняет rollback статуса.
func (h *CancelBookingErrorHandler) Handle(ctx context.Context, body []byte) error {
	var event messaging.CancelBookingJobError
	if err := json.Unmarshal(body, &event); err != nil {
		return fmt.Errorf("десериализация CancelBookingJobError: %w", err)
	}

	h.logger.Info("получено событие CancelBookingJobError",
		zap.String("requestId", event.RequestId),
		zap.String("reason", event.Reason),
	)

	if err := h.service.HandleCancelError(ctx, event.RequestId); err != nil {
		return fmt.Errorf("rollback отмены бронирования (requestId=%s): %w", event.RequestId, err)
	}

	h.logger.Info("отмена бронирования откачена через событие",
		zap.String("requestId", event.RequestId),
	)
	return nil
}
