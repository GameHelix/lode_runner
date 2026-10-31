-- The notifier's own data: which Telegram chat follows which tenant, the
-- one-time codes that link them, and how far each chat has been told about
-- each agent.

create table links (
  chat_id bigint primary key,
  tenant_id uuid not null,
  name text not null default '',
  linked_at timestamptz not null default now()
);
create index links_tenant on links (tenant_id);

-- Only a hash of each code is kept; the code itself is shown once.
create table link_codes (
  code_hash bytea primary key,
  tenant_id uuid not null,
  expires_at timestamptz not null,
  used_at timestamptz
);

create table cursors (
  chat_id bigint not null references links (chat_id) on delete cascade,
  agent_id uuid not null,
  seq bigint not null,
  primary key (chat_id, agent_id)
);

-- The next Telegram update to read, so a restart never handles a command
-- twice.
create table offsets (
  bot text primary key,
  next_update bigint not null
);
