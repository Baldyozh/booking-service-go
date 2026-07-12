package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"booking-service/app/models"
)

// BookingsRepository реализует models.BookingRepository.
type BookingsRepository struct {
	pool *pgxpool.Pool
}

// NewBookingsRepository создаёт новый экземпляр BookingsRepository.
func NewBookingsRepository(pool *pgxpool.Pool) *BookingsRepository {
	return &BookingsRepository{pool: pool}
}

// Create сохраняет новое бронирование.
func (r *BookingsRepository) Create(ctx context.Context, booking *models.Booking) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, queryInsertBooking,
		string(booking.Status()),
		booking.UserID(),
		booking.ResourceID(),
		booking.StartDate(),
		booking.EndDate(),
		booking.CreatedAt(),
	).Scan(&id)

	if err != nil {
		return 0, fmt.Errorf("создание бронирования: %w", err)
	}
	return id, nil
}

// GetByID возвращает бронирование по ID.
func (r *BookingsRepository) GetByID(ctx context.Context, id int64) (*models.Booking, error) {
	booking, err := r.scanBooking(r.pool.QueryRow(ctx, queryGetBookingByID, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.ErrBookingNotFound
		}
		return nil, fmt.Errorf("получение бронирования id=%d: %w", id, err)
	}
	return booking, nil
}

// GetStatistics возвращает агрегированную статистику за период (все вычисления в SQL).
func (r *BookingsRepository) GetStatistics(ctx context.Context, period models.StatisticsPeriod) (models.BookingStatistics, error) {
	dateFrom := period.DateFrom
	dateTo := period.DateTo

	var total int64
	if err := r.pool.QueryRow(ctx, queryCountBookingsInPeriod, dateFrom, dateTo).Scan(&total); err != nil {
		return models.BookingStatistics{}, fmt.Errorf("подсчёт бронирований за период: %w", err)
	}

	byStatus := make(map[models.BookingStatus]int64)
	rows, err := r.pool.Query(ctx, queryCountBookingsByStatusInPeriod, dateFrom, dateTo)
	if err != nil {
		return models.BookingStatistics{}, fmt.Errorf("подсчёт по статусам: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return models.BookingStatistics{}, fmt.Errorf("сканирование статуса: %w", err)
		}
		byStatus[models.BookingStatus(status)] = count
	}
	if err := rows.Err(); err != nil {
		return models.BookingStatistics{}, fmt.Errorf("итерация по статусам: %w", err)
	}

	topRows, err := r.pool.Query(ctx, queryTopResourcesInPeriod, dateFrom, dateTo)
	if err != nil {
		return models.BookingStatistics{}, fmt.Errorf("топ ресурсов: %w", err)
	}
	defer topRows.Close()

	topResources := make([]models.ResourceStatistics, 0)
	for topRows.Next() {
		var rs models.ResourceStatistics
		if err := topRows.Scan(&rs.ResourceID, &rs.BookingCount); err != nil {
			return models.BookingStatistics{}, fmt.Errorf("сканирование топ ресурсов: %w", err)
		}
		topResources = append(topResources, rs)
	}
	if err := topRows.Err(); err != nil {
		return models.BookingStatistics{}, fmt.Errorf("итерация по топ ресурсов: %w", err)
	}

	return models.BookingStatistics{
		TotalBookings: total,
		ByStatus:      byStatus,
		TopResources:  topResources,
	}, nil
}

// Update обновляет бронирование в хранилище.
func (r *BookingsRepository) Update(ctx context.Context, booking *models.Booking) error {
	tag, err := r.pool.Exec(ctx, queryUpdateBooking,
		string(booking.Status()),
		nullableStatus(booking.PreviousStatus()),
		nullableTime(booking.CancelCommandSentAt()),
		booking.ID(),
	)
	if err != nil {
		return fmt.Errorf("обновление бронирования id=%d: %w", booking.ID(), err)
	}
	if tag.RowsAffected() == 0 {
		return models.ErrBookingNotFound
	}
	return nil
}

// GetByFilter возвращает бронирования с фильтрацией и пагинацией.
func (r *BookingsRepository) GetByFilter(ctx context.Context, filter models.BookingFilter) ([]models.Booking, int64, error) {
	offset := (filter.Page - 1) * filter.Size

	var userID, resourceID *int64
	var status *string
	if filter.UserID != nil {
		userID = filter.UserID
	}
	if filter.ResourceID != nil {
		resourceID = filter.ResourceID
	}
	if filter.Status != nil {
		s := string(*filter.Status)
		status = &s
	}

	// Получение общего количества
	var totalCount int64
	err := r.pool.QueryRow(ctx, queryCountBookingsByFilter, userID, resourceID, status).Scan(&totalCount)
	if err != nil {
		return nil, 0, fmt.Errorf("подсчёт бронирований: %w", err)
	}

	// Получение данных
	rows, err := r.pool.Query(ctx, queryGetBookingsByFilter, userID, resourceID, status, filter.Size, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("получение бронирований по фильтру: %w", err)
	}
	defer rows.Close()

	var bookings []models.Booking
	for rows.Next() {
		booking, err := r.scanBookingFromRows(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("сканирование бронирования: %w", err)
		}
		bookings = append(bookings, *booking)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("итерация по строкам: %w", err)
	}

	return bookings, totalCount, nil
}

// GetAwaitingConfirmation возвращает бронирования, ожидающие подтверждения,
// с пессимистичной блокировкой FOR UPDATE SKIP LOCKED.
func (r *BookingsRepository) GetAwaitingConfirmation(ctx context.Context, limit int) ([]models.Booking, error) {
	rows, err := r.pool.Query(ctx, queryGetAwaitingConfirmation, limit)
	if err != nil {
		return nil, fmt.Errorf("получение бронирований для подтверждения: %w", err)
	}
	defer rows.Close()

	var bookings []models.Booking
	for rows.Next() {
		booking, err := r.scanBookingFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("сканирование бронирования: %w", err)
		}
		bookings = append(bookings, *booking)
	}

	return bookings, rows.Err()
}

// GetStuckCancellations возвращает бронирования в cancellation_pending,
// у которых команда отмены была отправлена раньше указанного порога.
func (r *BookingsRepository) GetStuckCancellations(ctx context.Context, sentBefore time.Time, limit int) ([]models.Booking, error) {
	rows, err := r.pool.Query(ctx, queryGetStuckCancellations, sentBefore, limit)
	if err != nil {
		return nil, fmt.Errorf("получение зависших отмен: %w", err)
	}
	defer rows.Close()

	var bookings []models.Booking
	for rows.Next() {
		booking, err := r.scanBookingFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("сканирование бронирования: %w", err)
		}
		bookings = append(bookings, *booking)
	}

	return bookings, rows.Err()
}

// scanBooking сканирует одну строку в доменный объект Booking.
func (r *BookingsRepository) scanBooking(row pgx.Row) (*models.Booking, error) {
	return scanBookingRow(row)
}

// scanBookingFromRows сканирует строку из pgx.Rows.
func (r *BookingsRepository) scanBookingFromRows(rows pgx.Rows) (*models.Booking, error) {
	return scanBookingRow(rows)
}

func scanBookingRow(row pgx.Row) (*models.Booking, error) {
	var (
		id                  int64
		status              string
		userID              int64
		resourceID          int64
		startDate           time.Time
		endDate             time.Time
		createdAt           time.Time
		previousStatus      *string
		cancelCommandSentAt *time.Time
	)

	err := row.Scan(
		&id, &status, &userID, &resourceID, &startDate, &endDate, &createdAt,
		&previousStatus, &cancelCommandSentAt,
	)
	if err != nil {
		return nil, err
	}

	var prevStatus models.BookingStatus
	if previousStatus != nil {
		prevStatus = models.BookingStatus(*previousStatus)
	}

	var sentAt time.Time
	if cancelCommandSentAt != nil {
		sentAt = *cancelCommandSentAt
	}

	return models.RestoreBooking(
		id, models.BookingStatus(status), userID, resourceID,
		startDate, endDate, createdAt, prevStatus, sentAt,
	), nil
}

func nullableStatus(s models.BookingStatus) any {
	if s == "" {
		return nil
	}
	return string(s)
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
