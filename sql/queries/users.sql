-- name: CreateUser :one
insert into users (email) values ($1) returning *;

-- name: DeleteAllUsers :exec
delete from users;