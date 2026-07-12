package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"booking-service/app/messaging"
	"booking-service/app/service"
)

// BookingConfirmedHandler обрабатывает события BookingJobConfirmed.
type BookingConfirmedHandler struct {
	service *service.BookingsService
	logger  *zap.Logger
}

// NewBookingConfirmedHandler создаёт новый обработчик.
func NewBookingConfirmedHandler(svc *service.BookingsService, logger *zap.Logger) *BookingConfirmedHandler {
	return &BookingConfirmedHandler{
		service: svc,
		logger:  logger,
	}
}

// Handle обрабатывает событие подтверждения бронирования.
func (h *BookingConfirmedHandler) Handle(ctx context.Context, body []byte) error {
	var event messaging.BookingJobConfirmed
	if err := json.Unmarshal(body, &event); err != nil {
		return fmt.Errorf("десериализация BookingJobConfirmed: %w", err)
	}

	bookingID, err := messaging.RequestIDToBookingID(event.RequestId)
	if err != nil {
		return fmt.Errorf("извлечение bookingId из RequestId: %w", err)
	}

	h.logger.Info("получено событие BookingJobConfirmed",
		zap.Int64("bookingId", bookingID),
		zap.Int64("catalogJobId", event.Id),
	)

	raceResolved, err := h.service.Confirm(ctx, bookingID)
	if err != nil {
		return fmt.Errorf("подтверждение бронирования %d: %w", bookingID, err)
	}

	if raceResolved {
		h.logger.Warn("обнаружен race condition: Catalog подтвердил бронирование во время отмены",
			zap.Int64("bookingId", bookingID),
			zap.Int64("catalogJobId", event.Id),
		)
	}

	h.logger.Info("бронирование подтверждено через событие", zap.Int64("bookingId", bookingID))
	return nil
}
