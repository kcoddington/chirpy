-- name: CreateUser :one
insert into users (email, hashed_password) values ($1, $2) returning *;

-- name: GetUserByEmail :one
select * from users where email = $1 limit 1;

-- name: GetUserFromRefreshToken :one
select users.* from users join refresh_tokens on users.id = refresh_tokens.user_id where refresh_tokens.token = $1 limit 1;

-- name: DeleteAllUsers :exec
delete from users;