# candylario

Candylario stopped on 4 November 2026. This file is its record: what it was, what it found and why it ended. The code is in the history, and the tag `candylario-final` marks the last commit that holds all of it.

## What it was

A radar over every liquid Hyperliquid perpetual that posted unusual moves to the Telegram channel [@candylario](https://t.me/candylario) as cards, each with what followed such moves since 2021 and the trade that record argued for. Accounts at candy.surf could link a Telegram chat and approve a Hyperliquid wallet (testnet only); one-tap trades from the cards were the next step, paid for by a builder fee on each trade.

- `trader/`: the engine: the radar, the wallets and trades, and the API the other two called
- `notifier/`: the Telegram bot and the channel
- `app/`: the website: the public pages, accounts and connecting Telegram
- `infra/`: one Hetzner server in OpenTofu, its settings, backups, rebuild and shutdown
- `research/`: hypothesis tests on Binance's public archive

Three Go services on one server, deployed by GitHub Actions on every push to main.

## Ten days

| 2026 | |
| --- | --- |
| 25 Oct | Starts as a paper-trading agent for US stocks whose decisions a language model makes; by the afternoon it trades crypto on Binance, then becomes a multi-tenant engine to be sold with bring-your-own keys and subscription tiers, with a Telegram bot. Named Wickwise, then Candylario. |
| 26 Oct | The app (accounts, tiers, onboarding, dashboards) on one Hetzner server, serving candy.surf over HTTPS with terms, privacy, risk and refund pages. |
| 29 Oct | What would customers pay for? Paddle's rules ban trading signals, and an agent's model bill (about $30–40 a month) outruns a small account. Binance's public archive is downloaded for research; chasing 15-minute jumps loses. **Turning point:** Hyperliquid, with a radar, one-tap trades and a builder fee per trade (tag `before-hyperliquid` in the first repositories). The radar starts posting to the channel. |
| 30 Oct | The research names the rules that held, and every card quotes their record. One-tap trades join the engine. Every AI feature is to be cut. |
| 31 Oct | The AI and Binance code goes. Five repositories become this one. Wallet connect, trade buttons, fills and a daily loss cap follow, all on testnet. The testnet run waits on a funded wallet and never happens. |
| 4 Nov | Stopped, and shut down the same day. |

## What it reached

- 3 subscribers to the channel, after about 480 alerts from 29 October to 4 November
- 2 accounts on candy.surf and 1 linked Telegram chat
- 0 trades, $0 revenue
- about 140 commits across six repositories
- one outage: 34 minutes on 31 October, from a server change that saved $0.50 a month

## What it found

On Binance's public archive (spot since 2017, perpetuals since 2020); the tables are in `research/results`.

- **Buying what is already moving loses.** After a 15-minute rise the median outcome was negative in every size and volume bucket on spot and in nearly every one on the perpetuals; on spot, every year from 2019 had a negative 4-hour median.
- **What held every year was small.** The clearest rules were fades: a 10% crash after a 20% pump went on to a 24-hour median of −6.2% (36% up), a rise with open interest up 10% to −2.8%, both in every year since 2021. Shorting pumps wins at the median and loses big in the tail.
- **Bounces faded.** A 10% crash of a coin that had not pumped bounced +5.9% at the 24-hour median over 2021–2026, and only +1.9% since 2025.
- **Popular recipes did not survive the data.** Four or five green candles predicted nothing; "institutional" confirmation (volume, buyers and open interest rising together) made a run slightly worse; very negative funding meant the fall went on, not a bounce.
- **The money rails were closed.** Paddle bans trading signals and crypto trading platforms, and Binance's data is licensed non-commercial (CC BY-NC-SA 4.0), so the tables the cards quoted could not be sold. A per-trade builder fee was left, and it needs traders who trade.

## Why it stopped

The founder was fed up with crypto, and the record agrees: no edge worth selling, three followers after hundreds of alerts, and every pivot started from a trading idea or a research result, never from a trader asking for something. After ten days of pivots, the infrastructure was further along than the product.

## The shutdown, 4 November 2026

The **shutdown** workflow (`infra/hetzner/shutdown.sh`) stopped the three services, posted a closing note to @candylario as its last post, destroyed the server, its IPv4 address, the backup volume, the firewalls and the SSH keys at Hetzner, and found the project empty. Every account, alert and backup went with them, and the repository's deploy secrets were deleted. The Binance archive stays on Hugging Face for research: [ismatsamadov/binance-usdt-archive](https://huggingface.co/datasets/ismatsamadov/binance-usdt-archive).

## Where the history is

- From 31 October: this repository, up to the tag `candylario-final`.
- Before that: the five first repositories, whose histories now sit under game rebuilds in GameHelix, tagged `before-hyperliquid` where they crossed that turn: trader in [gin_rummy](https://github.com/GameHelix/gin_rummy), notifier in [lunar_lander](https://github.com/GameHelix/lunar_lander), app in [billiards](https://github.com/GameHelix/billiards), infra in [quoridor](https://github.com/GameHelix/quoridor) and research in [hashi](https://github.com/GameHelix/hashi).
