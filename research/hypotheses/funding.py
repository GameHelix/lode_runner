"""Extreme funding: after a very high or very negative funding rate, where does the price go over the next day."""
import os, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import *

t0 = time.time()
con = connect()
con.execute("""
  CREATE TABLE px AS
  SELECT symbol, time_bucket(INTERVAL '1 hour', open_time) AS t, last(close ORDER BY open_time) AS c, sum(quote_volume) AS qv
  FROM um_klines_1m GROUP BY 1, 2
""")
con.execute("""
  CREATE TABLE ev AS
  WITH f AS (SELECT symbol, time_bucket(INTERVAL '1 hour', funding_time) AS t, rate, interval_hours FROM um_funding),
  x AS (SELECT p.symbol, p.t, p.c, p.qv, f.rate, f.interval_hours,
               lead(c, 8) OVER w AS c8, lead(c, 24) OVER w AS c24, lead(c, 72) OVER w AS c72,
               sum(qv) OVER (PARTITION BY p.symbol ORDER BY p.t ROWS BETWEEN 24 PRECEDING AND 1 PRECEDING) AS qv24
        FROM px p LEFT JOIN f ON f.symbol = p.symbol AND f.t = p.t WINDOW w AS (PARTITION BY p.symbol ORDER BY p.t))
  SELECT * FROM x WHERE rate IS NOT NULL AND c > 0 AND c72 IS NOT NULL AND qv24 >= 1e6
""")
rows = con.execute("""
  SELECT CASE WHEN rate >= 0.003 THEN 'funding >= 0.3% (longs pay)' WHEN rate >= 0.001 THEN '0.1% to 0.3%' WHEN rate > -0.0005 THEN 'normal' WHEN rate > -0.002 THEN '-0.05% to -0.2% (shorts pay)' ELSE '<= -0.2%' END AS funding,
         count(*) AS n, avg(c8 / c - 1) AS a8, median(c8 / c - 1) AS m8, avg(c24 / c - 1) AS a24, median(c24 / c - 1) AS m24, avg((c24 / c - 1 > 0)::INT) AS w24, avg(c72 / c - 1) AS a72, median(c72 / c - 1) AS m72
  FROM ev GROUP BY 1 ORDER BY min(rate) DESC
""").fetchall()
out = "Price change after a funding payment, by the rate paid (per interval, usually 8h), coins with $1M traded in the prior day:\n\n"
out += table([(f, n, pct(a), pct(b), pct(c), pct(d), f"{w:.0%}", pct(e), pct(g)) for f, n, a, b, c, d, w, e, g in rows], ["funding", "n", "8h avg", "8h med", "24h avg", "24h med", "24h up", "72h avg", "72h med"])
out += f"\n\n{time.time() - t0:.0f}s."
import json
keys = {"funding >= 0.3% (longs pay)": "high", "0.1% to 0.3%": "elevated", "normal": "normal", "-0.05% to -0.2% (shorts pay)": "negative", "<= -0.2%": "very_negative"}
with open(os.path.join(os.path.dirname(__file__), "..", "results", "funding.json"), "w") as f:
    json.dump({keys[r[0]]: {"n": r[1], "median_8h": round(r[3], 4), "median_24h": round(r[5], 4), "up_24h": round(r[6], 3), "median_72h": round(r[8], 4)} for r in rows}, f, indent=1, sort_keys=True)
write("funding", "Extreme funding rates and what follows (Binance USDT-M perps)", out)
