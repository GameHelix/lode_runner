-- The radar's alerts, kept so the engine serves them after a restart, its
-- numbering goes on, and the cooldowns they started still hold. They are
-- public and belong to no tenant.

create table radar_alerts (
  seq bigint primary key,
  at timestamptz not null,
  coin text not null,
  rule text not null,
  alert jsonb not null
);

grant select, insert on radar_alerts to trader_app;
