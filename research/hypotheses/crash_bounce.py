"""What makes a crash bounce: 15-minute drops of 10% or more, by market regime, prior pump, hour of day, and how far they dip first."""
import os, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import *

MARKET = sys.argv[1] if len(sys.argv) > 1 else "spot"  # spot or perps
SRC, COST, SUFFIX = ("um_klines_1m", PERP_COST, "_perps") if MARKET == "perps" else ("spot_klines_1m", SPOT_COST, "")

t0 = time.time()
con = connect()
bars15(con, SRC)
con.execute("""
  CREATE TABLE btc AS
  SELECT date_trunc('day', t) AS day, last(c ORDER BY t) AS c,
         avg(last(c ORDER BY t)) OVER (ORDER BY date_trunc('day', t) ROWS BETWEEN 29 PRECEDING AND CURRENT ROW) AS sma30
  FROM bars WHERE symbol = 'BTCUSDT' GROUP BY 1
""")
con.execute(f"""
  CREATE TABLE crash AS
  WITH x AS (
    SELECT symbol, t, c / o - 1 AS r15, qv / usual_qv AS vr, c / lag(c, 96) OVER w - 1 AS prior24h,
           lead(o, 1) OVER w AS entry, lead(c, 16) OVER w AS c4h, lead(c, 96) OVER w AS c24h,
           min(l) OVER (PARTITION BY symbol ORDER BY t ROWS BETWEEN 1 FOLLOWING AND 16 FOLLOWING) AS low4h,
           max(h) OVER (PARTITION BY symbol ORDER BY t ROWS BETWEEN 1 FOLLOWING AND 16 FOLLOWING) AS high4h,
           t - lag(t, 96) OVER w AS gap
    FROM bars WINDOW w AS (PARTITION BY symbol ORDER BY t))
  SELECT x.*, least(c4h / entry - 1, 5) - {COST} AS f4h, least(c24h / entry - 1, 5) - {COST} AS f24h,
         low4h / entry - 1 AS mae, least(high4h / entry - 1, 5) AS mfe, hour(t) AS hr, year(t) AS yr,
         CASE WHEN b.c >= b.sma30 THEN 'BTC above its 30-day average' ELSE 'BTC below its 30-day average' END AS regime
  FROM x JOIN btc b ON b.day = date_trunc('day', x.t) - INTERVAL '1 day'
  WHERE entry > 0 AND c24h IS NOT NULL AND gap = INTERVAL '1 day' AND r15 <= -0.10
""")
def section(title, group):
    rows = con.execute(f"SELECT {group} AS g, count(*), median(f4h), avg((f4h > 0)::INT), median(f24h), avg((f24h > 0)::INT), median(mae), quantile_cont(mae, 0.1) FROM crash GROUP BY 1 ORDER BY 1").fetchall()
    return f"\n\n{title}\n\n" + table([(g, n, pct(a), f"{b:.0%}", pct(c), f"{d:.0%}", pct(e), pct(f)) for g, n, a, b, c, d, e, f in rows], ["group", "n", "4h median", "4h up", "24h median", "24h up", "dip first (median)", "dip first (worst 10%)"])
out = f"Buying at the next bar after a 15-minute drop of 10% or more, net of costs. 'Dip first' is the lowest low in the next 4h against the entry: what a stop has to survive.\n"
out += section("By the market regime the day before:", "regime")
out += section("By what the coin did in the prior 24h:", "CASE WHEN prior24h >= 0.5 THEN 'was up 50%+ before the crash' WHEN prior24h >= 0.2 THEN 'was up 20-50%' WHEN prior24h >= -0.2 THEN 'was within ±20%' ELSE 'was already down 20%+' END")
out += section("By the volume of the crash bar:", "CASE WHEN vr < 2 THEN '<2x usual' WHEN vr < 5 THEN '2-5x' WHEN vr < 20 THEN '5-20x' ELSE '20x+' END")
out += section("By hour of day (UTC):", "CASE WHEN hr < 4 THEN '00-04' WHEN hr < 8 THEN '04-08' WHEN hr < 12 THEN '08-12' WHEN hr < 16 THEN '12-16' WHEN hr < 20 THEN '16-20' ELSE '20-24' END")
out += section("By period:", "CASE WHEN yr <= 2020 THEN '2017-2020' WHEN yr <= 2022 THEN '2021-2022' WHEN yr <= 2024 THEN '2023-2024' ELSE '2025-2026' END")
rows = con.execute("SELECT count(*), avg((mae <= -0.05)::INT), avg((mae <= -0.10)::INT), avg((mfe >= 0.05)::INT), avg((mfe >= 0.10)::INT) FROM crash").fetchone()
out += f"\n\nAll {rows[0]} crashes: {rows[1]:.0%} dip a further 5% or more within 4h, {rows[2]:.0%} a further 10%; {rows[3]:.0%} touch +5% within 4h, {rows[4]:.0%} touch +10%.\n\n{time.time() - t0:.0f}s."
import json
groups = con.execute("""
  SELECT CASE WHEN prior24h >= 0.5 THEN 'pumped50' WHEN prior24h >= 0.2 THEN 'pumped20' WHEN prior24h >= -0.2 THEN 'flat' ELSE 'beaten' END,
         count(*), median(f4h), avg((f4h > 0)::INT), median(f24h), avg((f24h > 0)::INT) FROM crash GROUP BY 1""").fetchall()
with open(os.path.join(os.path.dirname(__file__), "..", "results", "crash_bounce" + SUFFIX + ".json"), "w") as f:
    json.dump({k: {"n": n, "median_4h": round(a, 4), "up_4h": round(b, 3), "median_24h": round(c, 4), "up_24h": round(d, 3)} for k, n, a, b, c, d in groups}, f, indent=1, sort_keys=True)
write("crash_bounce" + SUFFIX, "What makes a crash bounce (USDT-M perpetuals, 2020–2026)" if MARKET == "perps" else "What makes a crash bounce (spot, 2017–2026)", out)
