-- What each account's trading did: the orders the engine placed for it,
-- every fill on the account, the notices those fills raise for its chats,
-- and the limits the account sets itself.

create table orders (
  tenant_id uuid not null references tenants (id),
  oid bigint not null,
  kind text not null check (kind in ('open', 'stop', 'take_profit', 'close')),
  coin text not null,
  at timestamptz not null default now(),
  primary key (tenant_id, oid)
);

create table fills (
  tenant_id uuid not null references tenants (id),
  tid bigint not null,
  oid bigint not null,
  at timestamptz not null,
  coin text not null,
  dir text not null,
  buy boolean not null,
  size double precision not null,
  price double precision not null,
  closed_pnl double precision not null,
  fee double precision not null,
  primary key (tenant_id, tid)
);
create index fills_tenant_at on fills (tenant_id, at);

create table notices (
  seq bigserial primary key,
  tenant_id uuid not null references tenants (id),
  at timestamptz not null default now(),
  text text not null
);

create table limits (
  tenant_id uuid primary key references tenants (id),
  max_usd double precision not null,
  max_leverage int not null,
  daily_loss_usd double precision not null
);

alter table orders enable row level security;
alter table orders force row level security;
create policy tenant_isolation on orders using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
alter table fills enable row level security;
alter table fills force row level security;
create policy tenant_isolation on fills using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
alter table notices enable row level security;
alter table notices force row level security;
create policy tenant_isolation on notices using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
alter table limits enable row level security;
alter table limits force row level security;
create policy tenant_isolation on limits using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

-- The two views across accounts: which accounts have a wallet to watch,
-- and the notices of every account in order, for the notifier.
create function wallet_tenants() returns setof uuid
language sql stable security definer set search_path = public as $$
  select distinct c.tenant_id from credentials c join tenants t on t.id = c.tenant_id
  where c.kind = 'hyperliquid' and c.revoked_at is null and t.status = 'active'
$$;

create function notices_after(after bigint, n int) returns table (seq bigint, tenant_id uuid, at timestamptz, text text)
language sql stable security definer set search_path = public as $$
  select seq, tenant_id, at, text from notices where seq > after order by seq limit n
$$;

create function last_notice() returns bigint
language sql stable security definer set search_path = public as $$
  select coalesce(max(seq), 0) from notices
$$;

revoke all on function wallet_tenants() from public;
revoke all on function notices_after(bigint, int) from public;
revoke all on function last_notice() from public;
grant execute on function wallet_tenants() to trader_app;
grant execute on function notices_after(bigint, int) to trader_app;
grant execute on function last_notice() to trader_app;
grant select, insert on orders, fills, notices to trader_app;
grant usage on sequence notices_seq_seq to trader_app;
grant select, insert, update on limits to trader_app;
