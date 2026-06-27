package models

import "time"

// BookingStatus представляет статус бронирования.
type BookingStatus string

const (
	BookingStatusAwaitsConfirmation  BookingStatus = "awaits_confirmation"
	BookingStatusConfirmed           BookingStatus = "confirmed"
	BookingStatusCancelled           BookingStatus = "cancelled"
	BookingStatusCancellationPending BookingStatus = "cancellation_pending"
)

// IsValid проверяет, что статус принадлежит допустимому множеству.
func (s BookingStatus) IsValid() bool {
	switch s {
	case BookingStatusAwaitsConfirmation, BookingStatusConfirmed, BookingStatusCancelled, BookingStatusCancellationPending:
		return true
	default:
		return false
	}
}

// Booking -- доменная сущность бронирования.
// Поля неэкспортируемые для обеспечения инкапсуляции.
type Booking struct {
	id                  int64
	status              BookingStatus
	userID              int64
	resourceID          int64
	startDate           time.Time
	endDate             time.Time
	createdAt           time.Time
	previousStatus      BookingStatus // сохраняется при BeginCancellation для rollback
	cancelCommandSentAt time.Time     // время отправки команды отмены в Catalog
}

func (b *Booking) ID() int64             { return b.id }
func (b *Booking) Status() BookingStatus { return b.status }
func (b *Booking) UserID() int64         { return b.userID }
func (b *Booking) ResourceID() int64     { return b.resourceID }
func (b *Booking) StartDate() time.Time  { return b.startDate }
func (b *Booking) EndDate() time.Time    { return b.endDate }
func (b *Booking) CreatedAt() time.Time  { return b.createdAt }
func (b *Booking) PreviousStatus() BookingStatus {
	return b.previousStatus
}
func (b *Booking) CancelCommandSentAt() time.Time {
	return b.cancelCommandSentAt
}

// NewBooking создаёт новое бронирование в статусе AwaitsConfirmation.
func NewBooking(userID, resourceID int64, startDate, endDate time.Time) (*Booking, error) {
	if userID <= 0 {
		return nil, ErrInvalidUserID
	}
	if resourceID <= 0 {
		return nil, ErrInvalidResourceID
	}
	if startDate.IsZero() || endDate.IsZero() {
		return nil, ErrInvalidDateRange
	}
	if !endDate.After(startDate) {
		return nil, ErrEndDateBeforeStartDate
	}

	return &Booking{
		status:     BookingStatusAwaitsConfirmation,
		userID:     userID,
		resourceID: resourceID,
		startDate:  startDate,
		endDate:    endDate,
		createdAt:  time.Now(),
	}, nil
}

// Confirm подтверждает бронирование.
// Допустимый переход: AwaitsConfirmation -> Confirmed.
func (b *Booking) Confirm() error {
	if b.status != BookingStatusAwaitsConfirmation {
		return ErrInvalidStatusTransition
	}
	b.status = BookingStatusConfirmed
	return nil
}

// BeginCancellation начинает двухфазную отмену (Compensating Transaction).
// Допустимые переходы:
//   - AwaitsConfirmation -> CancellationPending
//   - Confirmed -> CancellationPending (только если StartDate > today)
//
// Сохраняет предыдущий статус и время отправки команды для возможного rollback.
func (b *Booking) BeginCancellation(today time.Time) error {
	switch b.status {
	case BookingStatusAwaitsConfirmation:
		b.previousStatus = b.status
		b.status = BookingStatusCancellationPending
		b.cancelCommandSentAt = time.Now()
		return nil
	case BookingStatusConfirmed:
		if !b.startDate.After(today) {
			return ErrCannotCancelPastBooking
		}
		b.previousStatus = b.status
		b.status = BookingStatusCancellationPending
		b.cancelCommandSentAt = time.Now()
		return nil
	default:
		return ErrInvalidStatusTransition
	}
}

// CompleteCancellation завершает отмену после успешной обработки командой Catalog.
// Допустимый переход: CancellationPending -> Cancelled.
func (b *Booking) CompleteCancellation() error {
	if b.status != BookingStatusCancellationPending {
		return ErrInvalidStatusTransition
	}
	b.status = BookingStatusCancelled
	b.previousStatus = ""
	b.cancelCommandSentAt = time.Time{}
	return nil
}

// RollbackCancellation откатывает отмену при ошибке обработки команды (DLQ).
// Допустимый переход: CancellationPending -> previousStatus.
func (b *Booking) RollbackCancellation() error {
	if b.status != BookingStatusCancellationPending {
		return ErrInvalidStatusTransition
	}
	if b.previousStatus == "" {
		return ErrInvalidStatusTransition
	}
	b.status = b.previousStatus
	b.previousStatus = ""
	b.cancelCommandSentAt = time.Time{}
	return nil
}

// CancelAsDenied отменяет бронирование при отказе Catalog при создании.
// Прямой переход без compensation: AwaitsConfirmation -> Cancelled.
func (b *Booking) CancelAsDenied() error {
	if b.status != BookingStatusAwaitsConfirmation {
		return ErrInvalidStatusTransition
	}
	b.status = BookingStatusCancelled
	return nil
}

// RestoreBooking восстанавливает Booking из данных хранилища.
// Используется только в слое storage для маппинга строк БД на доменный объект.
func RestoreBooking(
	id int64,
	status BookingStatus,
	userID, resourceID int64,
	startDate, endDate, createdAt time.Time,
	previousStatus BookingStatus,
	cancelCommandSentAt time.Time,
) *Booking {
	return &Booking{
		id:                  id,
		status:              status,
		userID:              userID,
		resourceID:          resourceID,
		startDate:           startDate,
		endDate:             endDate,
		createdAt:           createdAt,
		previousStatus:      previousStatus,
		cancelCommandSentAt: cancelCommandSentAt,
	}
}
