-- name: GetChirps :many
select * from chirps
order by
	case when @sort_direction::text = 'asc' OR @sort_direction::text = '' then created_at end asc,
	case when @sort_direction::text = 'desc' then created_at end desc;

-- name: GetChirpsByUserID :many
select * from chirps
where user_id = @user_id
order by
	case when @sort_direction::text = 'asc' OR @sort_direction::text = '' then created_at end asc,
	case when @sort_direction::text = 'desc' then created_at end desc;

-- name: GetChirpByID :one
select * from chirps where id = $1 limit 1;

-- name: CreateChirp :one
insert into chirps (body, user_id) values ($1, $2) returning *;

-- name: DeleteAllChirps :exec
delete from chirps;

-- name: DeleteChirp :exec
delete from chirps where id = $1;