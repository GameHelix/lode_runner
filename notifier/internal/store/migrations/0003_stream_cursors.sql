-- How far the notifier has passed on a stream of the engine's that is not
-- per chat, such as the notices of every account.

create table stream_cursors (
  name text primary key,
  seq bigint not null,
  updated_at timestamptz not null default now()
);
