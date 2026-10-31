-- The old trading agent's tables go: its agents, their versions and keys,
-- and its event log with the streams it was kept in, along with the
-- functions only they used and the tenants' entitlements from the plans.

drop table agent_credentials, agent_versions, events, streams, agents cascade;
drop function runnable_agents(), ensure_event_partition(date), forbid_change();
alter table tenants drop column entitlements;
