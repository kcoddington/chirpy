-- +goose up
create table refresh_tokens (
    token text primary key not null,
    created_at timestamp not null default now(),
    updated_at timestamp not null default now(),
    user_id uuid not null references users(id) on delete cascade,
    expires_at timestamp not null,
    revoked_at timestamp null
);

-- +goose down
drop table refresh_tokens;