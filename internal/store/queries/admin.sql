-- name: CreateAdminUser :one
INSERT INTO admin_users (username, password_hash) VALUES ($1, $2) RETURNING *;

-- name: GetAdminUserByUsername :one
SELECT * FROM admin_users WHERE username = $1;

-- name: GetAdminUser :one
SELECT * FROM admin_users WHERE id = $1;

-- name: ListAdminUsers :many
SELECT id, username, disabled, created_at, last_login FROM admin_users ORDER BY username;

-- name: CountAdminUsers :one
SELECT count(*) FROM admin_users;

-- name: UpdateAdminPassword :exec
UPDATE admin_users SET password_hash = $2 WHERE id = $1;

-- name: SetAdminDisabled :exec
UPDATE admin_users SET disabled = $2 WHERE id = $1;

-- name: TouchAdminLogin :exec
UPDATE admin_users SET last_login = now() WHERE id = $1;

-- name: DeleteAdminUser :exec
DELETE FROM admin_users WHERE id = $1;

-- name: CreateSession :one
INSERT INTO admin_sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3) RETURNING *;

-- name: GetSessionByTokenHash :one
SELECT s.id, s.user_id, s.expires_at, u.username, u.disabled
FROM admin_sessions s JOIN admin_users u ON u.id = s.user_id
WHERE s.token_hash = $1;

-- name: TouchSession :exec
UPDATE admin_sessions SET last_seen = now() WHERE id = $1;

-- name: DeleteSessionByTokenHash :exec
DELETE FROM admin_sessions WHERE token_hash = $1;

-- name: DeleteUserSessions :exec
DELETE FROM admin_sessions WHERE user_id = $1;

-- name: DeleteExpiredSessions :exec
DELETE FROM admin_sessions WHERE expires_at < now();
