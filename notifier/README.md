# notifier

The Candylario bot, [@CandylarioBot](https://t.me/CandylarioBot). It posts the engine's radar alerts to a Telegram channel and connects private chats to accounts with one-time links from the app. It talks to the engine only through the [engine API](../trader/api/openapi.yaml).

- **Radar:** with `RADAR_CHANNEL_ID` set, every alert the engine raises goes to that channel as it is raised, in order, within Telegram's rate limits, retried until delivered and never twice. A restart starts from the latest alert.
- **Chats:** a private chat connects with a one-time link from the app and leaves with /disconnect; group messages are ignored.
- **Data:** its own Postgres database: which chat follows which tenant, the link codes (hashed), and the next Telegram update to read.

## Run

```sh
cp .env.example .env       # TELEGRAM_BOT_TOKEN from BotFather; ENGINE_NOTIFIER_TOKEN as in the engine (its token for the notifier); NOTIFIER_API_TOKEN (openssl rand -hex 32); POSTGRES_PASSWORD; RADAR_CHANNEL_ID, optional, a channel the bot posts the engine's radar alerts to (add the bot as an admin)
docker compose up -d --build                                  # needs the shared network: docker network create candylario
docker compose run --rm notifier link --tenant TENANT_ID      # a one-time link that connects a Telegram chat
```

The app connects chats through the notifier's API on `127.0.0.1:8081` (`notifier:8080` on the shared network) with `Authorization: Bearer $NOTIFIER_API_TOKEN`: `POST /v1/tenants/ID/links` gives a one-time link, `GET /v1/tenants/ID/chats` lists the chats connected and `DELETE /v1/tenants/ID/chats/CHAT` disconnects one. The contract is [api/openapi.yaml](api/openapi.yaml).

On a server prepared by [infra](../infra): `./deploy.sh notifier deploy@SERVER_IP` from the repository root deploys the pushed commit, tagged with it and reported by `/v1/health`, and `./deploy.sh notifier --rollback deploy@SERVER_IP` runs the previous image again.

Tests need Postgres: `TEST_DATABASE_URL=postgres://... go test ./...`
