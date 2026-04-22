-- name: CreateRefreshToken :one
insert into refresh_tokens (token, user_id, expires_at, revoked_at) values ($1, $2, $3, $4) returning *;

-- name: GetRefreshTokenByToken :one
select * from refresh_tokens where token = $1 limit 1;

-- name: RevokeRefreshToken :exec
update refresh_tokens set revoked_at = now() where token = $1;