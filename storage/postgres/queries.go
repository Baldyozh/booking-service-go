package postgres

const (
	queryInsertBooking = `
		INSERT INTO bookings (status, user_id, resource_id, start_date, end_date, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`

	queryGetBookingByID = `
		SELECT id, status, user_id, resource_id, start_date, end_date, created_at,
		       previous_status, cancel_command_sent_at
		FROM bookings
		WHERE id = $1`

	queryUpdateBooking = `
		UPDATE bookings
		SET status = $1,
		    previous_status = $2,
		    cancel_command_sent_at = $3
		WHERE id = $4`

	queryGetBookingsByFilter = `
		SELECT id, status, user_id, resource_id, start_date, end_date, created_at,
		       previous_status, cancel_command_sent_at
		FROM bookings
		WHERE ($1::BIGINT IS NULL OR user_id = $1)
		  AND ($2::BIGINT IS NULL OR resource_id = $2)
		  AND ($3::VARCHAR IS NULL OR status = $3)
		ORDER BY id DESC
		LIMIT $4 OFFSET $5`

	queryCountBookingsByFilter = `
		SELECT COUNT(*)
		FROM bookings
		WHERE ($1::BIGINT IS NULL OR user_id = $1)
		  AND ($2::BIGINT IS NULL OR resource_id = $2)
		  AND ($3::VARCHAR IS NULL OR status = $3)`

	queryGetAwaitingConfirmation = `
		SELECT id, status, user_id, resource_id, start_date, end_date, created_at,
		       previous_status, cancel_command_sent_at
		FROM bookings
		WHERE status = 'awaits_confirmation'
		ORDER BY created_at ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED`

	// Фильтр по created_at: dateFrom и dateTo включительно (dateTo + 1 день как верхняя граница).
	statisticsDateFilter = `
		created_at >= $1::date
		AND created_at < ($2::date + INTERVAL '1 day')`

	queryCountBookingsInPeriod = `
		SELECT COUNT(*)
		FROM bookings
		WHERE ` + statisticsDateFilter

	queryCountBookingsByStatusInPeriod = `
		SELECT s.status, COALESCE(c.cnt, 0)
		FROM (
			VALUES
				('awaits_confirmation'),
				('confirmed'),
				('cancelled'),
				('cancellation_pending')
		) AS s(status)
		LEFT JOIN (
			SELECT status, COUNT(*) AS cnt
			FROM bookings
			WHERE ` + statisticsDateFilter + `
			GROUP BY status
		) AS c ON c.status = s.status
		ORDER BY s.status`

	queryTopResourcesInPeriod = `
		SELECT resource_id, COUNT(*) AS booking_count
		FROM bookings
		WHERE ` + statisticsDateFilter + `
		GROUP BY resource_id
		ORDER BY booking_count DESC, resource_id ASC
		LIMIT 5`
)
