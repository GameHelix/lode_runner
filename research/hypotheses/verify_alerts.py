"""Check the engine's recent alerts against Hyperliquid's own candles: were the quoted moves right, and what did the coin do after each alert?"""
import json, sys, time, urllib.request
from datetime import datetime, timezone

API = "https://api.hyperliquid.xyz/info"
def info(body):
    req = urllib.request.Request(API, data=json.dumps(body).encode(), headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)

alerts = json.load(open(sys.argv[1]))["alerts"]
now_ms = int(time.time() * 1000)
print(f"{'time':5} {'rule':20} {'coin':7} {'quoted 15m':>10} {'actual':>7} {'quoted 1h':>9} {'actual':>7} {'since alert':>11} {'high':>6} {'low':>6} {'suggested':>9} {'so far':>7}")
for a in alerts:
    t = datetime.fromisoformat(a["at"].replace("Z", "+00:00"))
    t_ms = int(t.timestamp() * 1000)
    ks = info({"type": "candleSnapshot", "req": {"coin": a["coin"], "interval": "1m", "startTime": t_ms - 70 * 60000, "endTime": now_ms}})
    time.sleep(0.4)
    c = {k["t"]: (float(k["o"]), float(k["h"]), float(k["l"]), float(k["c"])) for k in ks}
    def close_at(ms):
        m = (ms // 60000) * 60000
        for back in range(0, 5):
            if m - back * 60000 in c:
                return c[m - back * 60000][3]
        return None
    p_at, p15, p1h = close_at(t_ms), close_at(t_ms - 15 * 60000), close_at(t_ms - 60 * 60000)
    after = [v for k, v in c.items() if k > t_ms]
    act15 = f"{(p_at / p15 - 1) * 100:+.1f}%" if p_at and p15 else "n/a"
    act1h = f"{(p_at / p1h - 1) * 100:+.1f}%" if p_at and p1h else "n/a"
    if after and a["price"]:
        last = after[-1][3]; hi = max(v[1] for v in after); lo = min(v[2] for v in after)
        since, hi_s, lo_s = f"{(last / a['price'] - 1) * 100:+.1f}%", f"{(hi / a['price'] - 1) * 100:+.1f}%", f"{(lo / a['price'] - 1) * 100:+.1f}%"
    else:
        since = hi_s = lo_s = "n/a"
    sug = a.get("suggestion") or {}
    side = sug.get("side", "-")
    verdict = "-"
    if side in ("long", "short") and after:
        r = last / a["price"] - 1
        verdict = ("win" if (r > 0) == (side == "long") else "loss") + f" {abs(r) * 100:.1f}%"
    print(f"{t:%H:%M} {a['rule'][:20]:20} {a['coin']:7} {a['change_15m'] * 100:>+9.1f}% {act15:>7} {a['change_1h'] * 100:>+8.1f}% {act1h:>7} {since:>11} {hi_s:>6} {lo_s:>6} {side:>9} {verdict:>7}")
