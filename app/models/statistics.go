package models

import "time"

// StatisticsPeriod задаёт диапазон дат для агрегации (включительно с обеих сторон).
type StatisticsPeriod struct {
	DateFrom time.Time
	DateTo   time.Time
}

// BookingStatistics содержит агрегированные данные по бронированиям за период.
type BookingStatistics struct {
	TotalBookings int64
	ByStatus      map[BookingStatus]int64
	TopResources  []ResourceStatistics
}

// ResourceStatistics — количество бронирований по ресурсу.
type ResourceStatistics struct {
	ResourceID   int64
	BookingCount int64
}
