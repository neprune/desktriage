-- name: GetTicketState :one
SELECT * FROM ticket_state WHERE ticket_id = ?;

-- name: ListTicketStates :many
SELECT * FROM ticket_state ORDER BY priority DESC, updated_at DESC;

-- name: ListTodayTicketStates :many
SELECT * FROM ticket_state WHERE today = 1 ORDER BY priority DESC;

-- name: ListTriageableTicketIDs :many
SELECT ticket_id FROM ticket_state WHERE priority = 0 AND today = 0 AND blocked = 0;

-- name: UpsertTicketState :exec
INSERT INTO ticket_state (ticket_id, priority, blocked, note, review_after, today, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(ticket_id) DO UPDATE SET
    priority = excluded.priority,
    blocked = excluded.blocked,
    note = excluded.note,
    review_after = excluded.review_after,
    today = excluded.today,
    updated_at = excluded.updated_at;

-- name: DeleteTicketState :exec
DELETE FROM ticket_state WHERE ticket_id = ?;
