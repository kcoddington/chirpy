-- +goose Up
create table chirps (
    id uuid primary key default gen_random_uuid(),
    body text not null,
    created_at timestamp not null default now(),
    updated_at timestamp not null default now(),
    user_id uuid not null references users(id) on delete cascade
);

-- +goose Down
drop table chirps;