# candylario

A radar over every liquid Hyperliquid perpetual that posts unusual moves to Telegram, each with the record of what followed such moves since 2021 and the trade that record argues for, and one-tap trades from those alerts next. Live at [candy.surf](https://candy.surf) and [t.me/candylario](https://t.me/candylario).

- [`trader/`](trader): the engine: the radar, each account's agent wallet and trades, and the API the other two call
- [`notifier/`](notifier): the Telegram bot: the channel, and private chats connected to accounts
- [`app/`](app): the website: the public pages, accounts and connecting Telegram
- [`infra/`](infra): the server (OpenTofu, at Hetzner), its settings and backups
- [`research/`](research): hypothesis tests on the Binance archive

One Go module holds the three services; each builds its own image and runs as its own stack on one server. Settings live only on the server (`infra/README.md`, Settings).

Deploys run in GitHub Actions, never from a laptop. A push to main runs the tests and then ships the services it changed, engine first, with `deploy.sh`. The **deploy** workflow redeploys or rolls back one service by hand, and the **infra** workflow plans or applies OpenTofu. `./infra/actions-key.sh` gives Actions its SSH key to the server, once.

`make check` runs the tests locally; the Postgres ones need `TEST_DATABASE_URL`.
