"""After a 15-minute move, where does the price go? Spot, every coin since 2017, by move size and volume, net of costs. Forward returns are capped at +500%: beyond that they are data errors."""
import os, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import *

MARKET = sys.argv[1] if len(sys.argv) > 1 else "spot"  # spot or perps
SRC, COST, SUFFIX = ("um_klines_1m", PERP_COST, "_perps") if MARKET == "perps" else ("spot_klines_1m", SPOT_COST, "")

t0 = time.time()
con = connect()
bars15(con, SRC)
con.execute(f"""
  CREATE TABLE moves AS
  WITH x AS (
    SELECT symbol, t, c / o - 1 AS r15, qv / usual_qv AS vr,
           lead(o, 1) OVER w AS entry, lead(c, 4) OVER w AS c1h, lead(c, 16) OVER w AS c4h, lead(c, 96) OVER w AS c24h,
           t - lag(t, 1) OVER w AS gap
    FROM bars WINDOW w AS (PARTITION BY symbol ORDER BY t))
  SELECT *, least(c1h / entry - 1, 5) AS f1h, least(c4h / entry - 1, 5) AS f4h, least(c24h / entry - 1, 5) AS f24h, year(t) AS yr
  FROM x WHERE entry > 0 AND c24h IS NOT NULL AND gap = INTERVAL '15 minutes' AND abs(r15) >= 0.03
""")
rows = con.execute(f"""
  SELECT CASE WHEN r15 > 0 THEN 'up' ELSE 'down' END AS dir,
         CASE WHEN abs(r15) < 0.05 THEN '3-5%' WHEN abs(r15) < 0.10 THEN '5-10%' WHEN abs(r15) < 0.20 THEN '10-20%' ELSE '20%+' END AS size,
         CASE WHEN vr < 2 THEN '<2x' WHEN vr < 5 THEN '2-5x' ELSE '5x+' END AS volume,
         count(*) AS n,
         median(f1h) - {COST} AS med1h, avg(f4h) - {COST} AS avg4h, median(f4h) - {COST} AS med4h,
         avg((f4h > {COST})::INT) AS p_up4h, avg(f24h) - {COST} AS avg24h, median(f24h) - {COST} AS med24h,
         avg((f24h >= 0.05 + {COST})::INT) AS p_plus5, avg((f24h <= -0.05 - {COST})::INT) AS p_minus5
  FROM moves GROUP BY 1, 2, 3 ORDER BY 1 DESC, 2, 3
""").fetchall()
out = table([(d, s, v, n, pct(a), pct(b), pct(c), f"{p:.0%}", pct(e), pct(f), f"{g:.0%}", f"{h:.0%}") for d, s, v, n, a, b, c, p, e, f, g, h in rows],
            ["move", "size", "volume", "n", "1h med", "4h avg", "4h med", "4h up", "24h avg", "24h med", "24h ≥+5%", "24h ≤-5%"])
out += "\n\nBuying after an up move of 5% or more, by year (4h and 24h, net of costs):\n\n"
rows = con.execute(f"""
  SELECT yr, count(*) AS n, avg(f4h) - {COST}, median(f4h) - {COST}, avg((f4h > {COST})::INT), avg(f24h) - {COST}, median(f24h) - {COST}
  FROM moves WHERE r15 >= 0.05 GROUP BY 1 ORDER BY 1
""").fetchall()
out += table([(y, n, pct(a), pct(b), f"{p:.0%}", pct(c), pct(d)) for y, n, a, b, p, c, d in rows], ["year", "n", "4h avg", "4h med", "4h up", "24h avg", "24h med"])
out += "\n\nBuying the dip after a down move of 5% or more, by year:\n\n"
rows = con.execute(f"""
  SELECT yr, count(*) AS n, avg(f4h) - {COST}, median(f4h) - {COST}, avg((f4h > {COST})::INT), avg(f24h) - {COST}, median(f24h) - {COST}
  FROM moves WHERE r15 <= -0.05 GROUP BY 1 ORDER BY 1
""").fetchall()
out += table([(y, n, pct(a), pct(b), f"{p:.0%}", pct(c), pct(d)) for y, n, a, b, p, c, d in rows], ["year", "n", "4h avg", "4h med", "4h up", "24h avg", "24h med"])
base = con.execute("SELECT median(f), avg(f) FROM (SELECT lead(c, 16) OVER w / lead(o, 1) OVER w - 1 AS f FROM bars WHERE hash(symbol) % 10 = 0 WINDOW w AS (PARTITION BY symbol ORDER BY t)) WHERE f IS NOT NULL").fetchone()
out += f"\n\nBase rate, any coin any 15-minute bar (a tenth of the coins): 4h median {pct(base[0] - COST)}, average {pct(base[1] - COST)} net of costs.\n"
out += f"\n{time.time() - t0:.0f}s on {con.execute('SELECT count(DISTINCT symbol) FROM bars').fetchone()[0]} coins."
import json
first = con.execute(f"""
  SELECT CASE WHEN r15 > 0 THEN 'up' ELSE 'down' END || '|' || CASE WHEN abs(r15) < 0.05 THEN '3-5' WHEN abs(r15) < 0.10 THEN '5-10' WHEN abs(r15) < 0.20 THEN '10-20' ELSE '20+' END || '|' || CASE WHEN vr < 2 THEN '<2' WHEN vr < 5 THEN '2-5' ELSE '5+' END,
         count(*), median(f4h) - {COST}, avg((f4h > {COST})::INT), median(f24h) - {COST}, avg((f24h > {COST})::INT)
  FROM moves GROUP BY 1""").fetchall()
with open(os.path.join(os.path.dirname(__file__), "..", "results", "after_move" + SUFFIX + ".json"), "w") as f:
    json.dump({k: {"n": n, "median_4h": round(a, 4), "up_4h": round(b, 3), "median_24h": round(c, 4), "up_24h": round(d, 3)} for k, n, a, b, c, d in first}, f, indent=1, sort_keys=True)
write("after_move" + SUFFIX, "After a 15-minute move (USDT-M perpetuals, 2020–2026)" if MARKET == "perps" else "After a 15-minute move (spot, 2017–2026)", out)
