"""Open interest with price: when both rise over an hour, or diverge, what follows over 4h and 24h."""
import os, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import *

t0 = time.time()
con = connect()
bars15(con, "um_klines_1m")
con.execute("""
  CREATE TABLE oi AS
  SELECT symbol, time_bucket(INTERVAL '15 minutes', time) AS t, last(open_interest ORDER BY time) AS oi
  FROM um_metrics WHERE open_interest > 0 GROUP BY 1, 2
""")
con.execute(f"""
  CREATE TABLE joined AS
  WITH x AS (
    SELECT b.symbol, b.t, b.c, b.qv / b.usual_qv AS vr, o.oi,
           lag(b.c, 4) OVER w AS c_1h, lag(o.oi, 4) OVER w AS oi_1h,
           lead(b.o, 1) OVER w AS entry, lead(b.c, 16) OVER w AS c4h, lead(b.c, 96) OVER w AS c24h, b.t - lag(b.t, 4) OVER w AS gap
    FROM bars b JOIN oi o ON o.symbol = b.symbol AND o.t = b.t WINDOW w AS (PARTITION BY b.symbol ORDER BY b.t))
  SELECT symbol, t, c / c_1h - 1 AS p1h, oi / oi_1h - 1 AS d_oi, entry, c4h / entry - 1 AS f4h, c24h / entry - 1 AS f24h
  FROM x WHERE c_1h > 0 AND oi_1h > 0 AND entry > 0 AND c24h IS NOT NULL AND gap = INTERVAL '1 hour'
""")
rows = con.execute(f"""
  SELECT CASE WHEN p1h >= 0.02 THEN 'price up 2%+' WHEN p1h <= -0.02 THEN 'price down 2%+' ELSE 'price flat' END AS price,
         CASE WHEN d_oi >= 0.10 THEN 'OI up 10%+' WHEN d_oi >= 0.03 THEN 'OI up 3-10%' WHEN d_oi <= -0.10 THEN 'OI down 10%+' WHEN d_oi <= -0.03 THEN 'OI down 3-10%' ELSE 'OI flat' END AS oi,
         count(*) AS n, avg(f4h) AS a4, median(f4h) AS m4, avg((f4h > 0)::INT) AS w4, avg(f24h) AS a24, median(f24h) AS m24, avg((f24h > 0)::INT) AS w24
  FROM joined GROUP BY 1, 2 HAVING count(*) >= 500 ORDER BY 1, 2
""").fetchall()
out = "Forward returns of the coin after the hour (no position costs; a long pays 0.15% round trip, a short the same):\n\n"
out += table([(p, o, n, pct(a), pct(b), f"{w:.0%}", pct(c), pct(d), f"{e:.0%}") for p, o, n, a, b, w, c, d, e in rows], ["last hour", "open interest", "n", "4h avg", "4h med", "4h up", "24h avg", "24h med", "24h up"])
out += f"\n\n{time.time() - t0:.0f}s."
write("open_interest", "Open interest and price over an hour (Binance USDT-M perps)", out)
