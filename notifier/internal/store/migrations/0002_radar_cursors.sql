-- How far each chat that gets the radar's alerts has got, so a restart of
-- either side neither repeats an alert nor loses one. The agents' notice
-- cursors went with the agents.

create table radar_cursors (
  chat_id bigint primary key,
  seq bigint not null,
  updated_at timestamptz not null default now()
);

drop table cursors;
