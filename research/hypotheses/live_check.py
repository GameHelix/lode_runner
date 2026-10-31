"""Were the live alerts right? Reads the channel's posts (json: id, at, text), parses every alert the engine has posted in any of its formats, pulls Hyperliquid's own 1-minute candles, and reports what each coin did 1h, 4h and 8h after the alert, against BTC, and whether the suggestions paid; grouped by trigger, next to the record the card quoted."""
import json, os, re, statistics, sys, time, urllib.request
from collections import defaultdict
from datetime import datetime, timezone

API = "https://api.hyperliquid.xyz/info"
HORIZONS = [60, 240, 480]
RECORD = {  # the study's numbers the cards quote: (4h median, 4h up, 24h median, 24h up)
    "move up": (-0.006, 0.44, -0.014, 0.43), "move down": (0.006, 0.56, 0.010, 0.55),
    "funding flip": (-0.005, 0.43, -0.022, 0.38), "big hour up": (-0.009, 0.44, -0.025, 0.42), "big hour down": (0.008, 0.56, 0.010, 0.54),
    "big move up": (-0.006, 0.44, -0.014, 0.43), "big move down": (0.006, 0.56, 0.010, 0.55),
    "pump-and-crash": (-0.013, 0.46, -0.062, 0.36), "unpumped crash": (0.060, 0.70, 0.059, 0.65), "crowded rise": (-0.011, 0.40, -0.028, 0.36),
    "spike rejected": (-0.004, 0.48, -0.026, 0.42), "open interest swing": (None, None, None, None), "momentum run": (-0.004, 0.46, -0.004, 0.46),
}

def info(body):
    req = urllib.request.Request(API, data=json.dumps(body).encode(), headers={"Content-Type": "application/json"})
    for attempt in range(5):
        try:
            with urllib.request.urlopen(req, timeout=30) as r:
                return json.load(r)
        except Exception:
            time.sleep(2 * (attempt + 1))
    raise RuntimeError("hyperliquid down")

def parse(post):
    text, head = post["text"], post["text"].split("\n")[0]
    pct = lambda label: (lambda m: float(m.group(1)) / 100 if m else None)(re.search(r"([+-][\d.]+)% " + label if label != "15m" else r"([+-][\d.]+)%(?: in)? 15m", text) or re.search(r"15m ([+-][\d.]+)%" if label == "15m" else r"1h ([+-][\d.]+)%" if label == "in 1h" else r"24h ([+-][\d.]+)%", text))
    a = {"id": post["id"], "at": post["at"], "side": None, "stop": None}
    if " — " in head:  # the card: "🔁 FUNDING FLIP — BLUR  $0.008"
        m = re.match(r"^(\S+) (.+?) — (\S+)\s+\$([\d.,]+)", head)
        if not m:
            return None
        a.update(rule=m.group(2).lower(), coin=m.group(3), price=float(m.group(4).replace(",", "")))
    elif " · " in head and re.match(r"^(\S+) ([a-z\- ]+) · (\S+) ", head):  # "🚀 funding flip · TRX +0.9% in 15m, ..."
        m = re.match(r"^(\S+) ([a-z\- ]+) · (\S+) ", head)
        a.update(rule=m.group(2), coin=m.group(3))
    elif re.match(r"^(🔻|🚀) (\S+) ([+-][\d.]+)% in 15m", head):  # the first engine: a bare move
        m = re.match(r"^(🔻|🚀) (\S+) ", head)
        a.update(rule="move " + ("up" if m.group(1) == "🚀" else "down"), coin=m.group(2))
    else:
        return None
    if "price" not in a:
        m = re.search(r"· \$([\d.,]+)\s*$", head)
        a["price"] = float(m.group(1).replace(",", "")) if m else None
    a["ch15"], a["ch1h"], a["ch24h"] = pct("15m"), pct("in 1h"), pct("today")
    if a["rule"] in ("big hour", "big move"):
        a["rule"] += " up" if (a["ch1h"] or a["ch15"] or 0) > 0 else " down"
    m = re.search(r"💡 (long|short|LONG|SHORT)\b.*?(\d+)% stop|💡 (LONG|SHORT) · stop (\d+)%", text)
    if m:
        a["side"] = (m.group(1) or m.group(3)).lower(); a["stop"] = int(m.group(2) or m.group(4)) / 100
    return a

def candles(coin, start_ms, end_ms):
    out = {}
    t = start_ms
    while t < end_ms:
        ks = info({"type": "candleSnapshot", "req": {"coin": coin, "interval": "1m", "startTime": t, "endTime": end_ms}})
        if not ks:
            break
        for k in ks:
            out[k["t"]] = (float(k["o"]), float(k["h"]), float(k["l"]), float(k["c"]))
        last = max(k["t"] for k in ks)
        if last <= t or len(ks) < 4000:
            break
        t = last + 60000
        time.sleep(0.3)
    return out

def main():
    posts = json.load(open(sys.argv[1]))
    alerts = [a for a in (parse(p) for p in posts) if a]
    skipped = len(posts) - len(alerts)
    now_ms = int(time.time() * 1000)
    t_ms = lambda a: int(datetime.fromisoformat(a["at"].replace("Z", "+00:00")).timestamp() * 1000) // 60000 * 60000
    start = min(t_ms(a) for a in alerts) - 3600000
    series = {}
    for coin in sorted({a["coin"] for a in alerts} | {"BTC"}):
        series[coin] = candles(coin, start, now_ms)
        time.sleep(0.3)
    def px(coin, ms, what=3):
        c = series[coin]
        for back in range(0, 6):
            if ms - back * 60000 in c:
                return c[ms - back * 60000][what]
        return None
    rows = []
    for a in alerts:
        t0 = t_ms(a)
        p0 = px(a["coin"], t0); b0 = px("BTC", t0)
        if not p0 or not b0:
            continue
        r = {"quoted_vs_candle": a["price"] / p0 - 1 if a["price"] else None}
        for h in HORIZONS:
            if t0 + h * 60000 <= now_ms - 60000:
                p = px(a["coin"], t0 + h * 60000); b = px("BTC", t0 + h * 60000)
                r[h] = (p / p0 - 1, b / b0 - 1) if p and b else None
            else:
                r[h] = None
        after = [(k, v) for k, v in series[a["coin"]].items() if t0 < k <= t0 + 240 * 60000]
        r["mfe4h"] = max(v[1] for k, v in after) / p0 - 1 if after else None
        r["mae4h"] = min(v[2] for k, v in after) / p0 - 1 if after else None
        last_k = max(series[a["coin"]])
        r["so_far"] = (series[a["coin"]][last_k][3] / p0 - 1, series["BTC"][max(series["BTC"])][3] / b0 - 1, (last_k - t0) / 3600000)
        rows.append({**a, **{str(k): v for k, v in r.items()}})
    md = [f"# Live check of the channel's alerts\n", f"{len(alerts)} alerts posted {alerts[0]['at'][:16]}Z to {alerts[-1]['at'][:16]}Z ({skipped} posts were not alerts); checked {datetime.now(timezone.utc):%Y-%m-%d %H:%M}Z against Hyperliquid 1-minute candles. 'vs BTC' is the coin's move minus BTC's over the same window. The record column is what the card quoted (perps 2021–2026, 4h and 24h median and share up).\n"]
    q = [abs(r["quoted_vs_candle"]) for r in rows if r["quoted_vs_candle"] is not None]
    md.append(f"Quoted price vs the candle at the alert minute: median gap {statistics.median(q) * 100:.2f}%, worst {max(q) * 100:.2f}% ({len(q)} alerts).\n")
    md.append("| trigger | n | 1h med | 1h vs BTC | 4h med | 4h up | 4h vs BTC | 8h med | 8h vs BTC | worst 4h | record 4h | record 24h |")
    md.append("|---|---|---|---|---|---|---|---|---|---|---|---|")
    groups = defaultdict(list)
    for r in rows:
        groups[r["rule"]].append(r)
    def stat(rs, h, i=0):
        xs = [r[str(h)][i] for r in rs if r.get(str(h))]
        return xs
    def fmt(xs, up=False):
        if not xs:
            return "—"
        return f"{sum(x > 0 for x in xs) / len(xs) * 100:.0f}%" if up else f"{statistics.median(xs) * 100:+.1f}% ({len(xs)})"
    for rule, rs in sorted(groups.items(), key=lambda kv: -len(kv[1])):
        ex = lambda h: fmt([r[str(h)][0] - r[str(h)][1] for r in rs if r.get(str(h))])
        rec = RECORD.get(rule, (None,) * 4)
        rec4 = f"{rec[0] * 100:+.1f}% ({rec[1] * 100:.0f}% up)" if rec[0] is not None else "—"
        rec24 = f"{rec[2] * 100:+.1f}% ({rec[3] * 100:.0f}% up)" if rec[2] is not None else "—"
        worst = min((r["mae4h"] for r in rs if r["mae4h"] is not None), default=None)
        md.append(f"| {rule} | {len(rs)} | {fmt(stat(rs, 60))} | {ex(60)} | {fmt(stat(rs, 240))} | {fmt(stat(rs, 240), up=True)} | {ex(240)} | {fmt(stat(rs, 480))} | {ex(480)} | {worst * 100:+.1f}% | {rec4} | {rec24} |".replace("None%", "—"))
    md.append("")
    sug = [r for r in rows if r["side"]]
    if sug:
        md.append("## Suggestions\n")
        md.append("| at | trigger | coin | side | stop | 1h | 4h | 8h | so far (h) | stop hit in 4h | result |")
        md.append("|---|---|---|---|---|---|---|---|---|---|---|")
        wins = 0
        for r in sug:
            sign = 1 if r["side"] == "long" else -1
            f = lambda h: f"{r[str(h)][0] * 100:+.1f}%" if r.get(str(h)) else "—"
            sf, _, hrs = r["so_far"]
            adverse = r["mae4h"] if r["side"] == "long" else -r["mfe4h"] if r["mfe4h"] is not None else None
            hit = adverse is not None and adverse <= -r["stop"]
            ref = r["240"][0] if r.get("240") else sf
            win = (sign * ref > 0) and not hit
            wins += win
            md.append(f"| {r['at'][11:16]} | {r['rule']} | {r['coin']} | {r['side']} | {r['stop'] * 100:.0f}% | {f(60)} | {f(240)} | {f(480)} | {sf * 100:+.1f}% ({hrs:.0f}) | {'yes' if hit else 'no'} | {'win' if win else 'loss'} {abs(ref) * 100:.1f}% |")
        md.append(f"\n{wins} of {len(sug)} suggestions were right at 4h (or so far, if 4h has not passed), stops counted as losses.\n")
    btc = series["BTC"]; ks = sorted(btc)
    md.append(f"BTC over the window: {(btc[ks[-1]][3] / btc[ks[0]][0] - 1) * 100:+.1f}% from {datetime.fromtimestamp(ks[0] / 1000, timezone.utc):%H:%M}Z to {datetime.fromtimestamp(ks[-1] / 1000, timezone.utc):%H:%M}Z.\n")
    os.makedirs("results", exist_ok=True)
    open("results/live_check.md", "w").write("\n".join(md))
    json.dump(rows, open("results/live_check.json", "w"), default=str)
    print("\n".join(md))

if __name__ == "__main__":
    main()
