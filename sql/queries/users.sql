-- name: CreateUser :one
insert into users (email, hashed_password) values ($1, $2) returning *;

-- name: GetUserByEmail :one
select * from users where email = $1 limit 1;

-- name: DeleteAllUsers :exec
delete from users;