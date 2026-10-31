"""The last hours on Hyperliquid, live: every run of four green 15-minute candles and what the price did after it. No key needed."""
import json, sys, time, urllib.request
from datetime import datetime, timezone

HOURS = int(sys.argv[1]) if len(sys.argv) > 1 else 12
API = "https://api.hyperliquid.xyz/info"

def info(body):
    req = urllib.request.Request(API, data=json.dumps(body).encode(), headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)

meta, ctxs = info({"type": "metaAndAssetCtxs"})
coins = [a["name"] for a, c in zip(meta["universe"], ctxs) if not a.get("isDelisted") and float(c["dayNtlVlm"]) >= 1e6]
now = int(time.time() * 1000)
rows, scanned = [], 0
for coin in coins:
    candles = info({"type": "candleSnapshot", "req": {"coin": coin, "interval": "15m", "startTime": now - (HOURS + 1) * 3600 * 1000, "endTime": now}})
    time.sleep(0.6)
    c = [(k["t"], float(k["o"]), float(k["h"]), float(k["l"]), float(k["c"])) for k in candles]
    c = c[:-1]  # the candle still forming
    scanned += 1
    i = 4
    while i < len(c):
        if all(c[j][4] > c[j][1] for j in range(i - 4, i)):
            entry = c[i][1]
            run = c[i - 1][4] / c[i - 4][1] - 1
            after = c[i:]
            best = max(k[2] for k in after) / entry - 1
            worst = min(k[3] for k in after) / entry - 1
            last = after[-1][4] / entry - 1
            rows.append((coin, datetime.fromtimestamp(c[i][0] / 1000, timezone.utc), run, entry, best, worst, last, len(after)))
            i += 4  # one signal per run
        else:
            i += 1
print(f"Hyperliquid, last {HOURS} hours, {scanned} coins with $1M+ traded: {len(rows)} runs of four green 15-minute candles.")
print(f"{'coin':8} {'entry (UTC)':17} {'4 candles':>9} {'best after':>10} {'worst after':>11} {'now':>7} {'candles after':>13}")
for coin, t, run, entry, best, worst, last, n in sorted(rows, key=lambda r: -r[4]):
    print(f"{coin:8} {t:%m-%d %H:%M}        {run*100:+8.1f}% {best*100:+9.1f}% {worst*100:+10.1f}% {last*100:+6.1f}% {n:13d}")
if rows:
    print(f"\ntouched +10% after: {sum(r[4] >= 0.10 for r in rows)} of {len(rows)}; touched -10%: {sum(r[5] <= -0.10 for r in rows)}; above entry now: {sum(r[6] > 0 for r in rows)}")
