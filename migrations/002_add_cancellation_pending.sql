-- +goose Up
ALTER TABLE bookings
    ADD COLUMN previous_status VARCHAR(30),
    ADD COLUMN cancel_command_sent_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE bookings
    DROP COLUMN IF EXISTS cancel_command_sent_at,
    DROP COLUMN IF EXISTS previous_status;
