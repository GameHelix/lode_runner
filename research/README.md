# research

Hypothesis tests on the Binance archive (1-minute candles for every USDT pair since 2017, futures candles, open interest, funding), read from its copy on Hugging Face, [ismatsamadov/binance-usdt-archive](https://huggingface.co/datasets/ismatsamadov/binance-usdt-archive). A full scan downloads tens of gigabytes; for repeated runs, download the sets once and point `BINANCE_DATA` at that folder. Each script in `hypotheses/` tests one idea and writes a table to `results/`. The radar uses four of them:
- `after_move_perps.json`, `crash_bounce_perps.json`, `funding.json` and `triggers.json` become `trader/internal/radar/history_*.json`.
- `five_green_confirmed.md` gives the momentum-run record.

    python3 -m venv .venv && .venv/bin/pip install -r requirements.txt
    .venv/bin/python -I hypotheses/after_move.py

Costs are round trips: 0.3% on spot (0.1% fee and 0.05% slippage per side), 0.15% on perps.

## Live check

`python3 -I hypotheses/engine_alerts.py deploy@91.99.222.102` pulls every alert the engine has kept into alerts.json (`channel_posts.py candylario` reads the channel's posts instead, into posts.json); `python3 -I hypotheses/live_check.py alerts.json` reads every alert back and checks it against Hyperliquid's candles: results/live_check.md.
