-- name: ListSprintAnchors :many
SELECT * FROM sprint_anchor ORDER BY year DESC, sprint_number ASC;

-- name: GetSprintAnchorsForYear :many
SELECT * FROM sprint_anchor WHERE year = ? ORDER BY sprint_number ASC;

-- name: UpsertSprintAnchor :exec
INSERT INTO sprint_anchor (sprint_number, year, start_date)
VALUES (?, ?, ?)
ON CONFLICT(year, sprint_number) DO UPDATE SET
    start_date = excluded.start_date;

-- name: DeleteSprintAnchor :exec
DELETE FROM sprint_anchor WHERE year = ? AND sprint_number = ?;
