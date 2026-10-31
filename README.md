# candylario

A radar over every liquid Hyperliquid perpetual that posts unusual moves to Telegram, each with the record of what followed such moves since 2021 and the trade that record argues for, and one-tap trades from those alerts next. Live at [candy.surf](https://candy.surf) and [t.me/candylario](https://t.me/candylario).

- [`trader/`](trader): the engine: the radar, each account's agent wallet and trades, and the API the other two call
- [`notifier/`](notifier): the Telegram bot: the channel, and private chats connected to accounts
- [`app/`](app): the website: the public pages, accounts and connecting Telegram
- [`infra/`](infra): the server (OpenTofu, at Hetzner), its settings and backups
- [`research/`](research): hypothesis tests on the Binance archive

One Go module holds the three services; each builds its own image and runs as its own stack on one server. Settings live only on the server (`infra/README.md`, Settings).

```sh
make check                                              # tests; the Postgres ones need TEST_DATABASE_URL
./deploy.sh trader|notifier|app deploy@91.99.222.102    # ships the pushed commit
```

Until October 31, 2026 these were five repositories, candylario/trader, notifier, app, infra and research; their history stays there.
