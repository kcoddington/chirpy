-- name: GetChirps :many
select * from chirps;

-- name: GetChirpsByUserID :many
select * from chirps where user_id = $1;

-- name: GetChirpByID :one
select * from chirps where id = $1 limit 1;

-- name: CreateChirp :one
insert into chirps (body, user_id) values ($1, $2) returning *;

-- name: DeleteAllChirps :exec
delete from chirps;

-- name: DeleteChirp :exec
delete from chirps where id = $1;