-- name: ListAccountManagers :many
SELECT * FROM account_manager ORDER BY company_id, agent_id;

-- name: ListAccountManagersByCompany :many
SELECT * FROM account_manager WHERE company_id = ? ORDER BY agent_id;

-- name: InsertAccountManager :exec
INSERT OR IGNORE INTO account_manager (company_id, agent_id) VALUES (?, ?);

-- name: DeleteAccountManager :exec
DELETE FROM account_manager WHERE company_id = ? AND agent_id = ?;
