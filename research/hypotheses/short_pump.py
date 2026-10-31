"""Short the pump: after a 15-minute rise on perps, a short held 4h or 24h, net of costs and with funding received or paid."""
import os, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import *

t0 = time.time()
con = connect()
bars15(con, "um_klines_1m")
con.execute(f"""
  CREATE TABLE moves AS
  WITH x AS (
    SELECT symbol, t, c / o - 1 AS r15, qv / usual_qv AS vr,
           lead(o, 1) OVER w AS entry, lead(t, 1) OVER w AS t_in, lead(c, 16) OVER w AS c4h, lead(t, 16) OVER w AS t4h, lead(c, 96) OVER w AS c24h, lead(t, 96) OVER w AS t24h,
           t - lag(t, 1) OVER w AS gap
    FROM bars WINDOW w AS (PARTITION BY symbol ORDER BY t))
  SELECT * FROM x WHERE entry > 0 AND c24h IS NOT NULL AND gap = INTERVAL '15 minutes' AND r15 >= 0.03
""")
con.execute("""
  CREATE TABLE funded AS
  SELECT m.symbol, m.t, m.r15, m.vr, m.entry, m.c4h, m.c24h, year(m.t) AS yr,
         coalesce(sum(f.rate) FILTER (WHERE f.funding_time <= m.t4h), 0) AS fund4h,
         coalesce(sum(f.rate), 0) AS fund24h
  FROM moves m LEFT JOIN um_funding f ON f.symbol = m.symbol AND f.funding_time > m.t_in AND f.funding_time <= m.t24h
  GROUP BY ALL
""")
q = lambda where: con.execute(f"""
  SELECT CASE WHEN r15 < 0.05 THEN '3-5%' WHEN r15 < 0.10 THEN '5-10%' WHEN r15 < 0.20 THEN '10-20%' ELSE '20%+' END AS size,
         CASE WHEN vr < 2 THEN '<2x' WHEN vr < 5 THEN '2-5x' ELSE '5x+' END AS volume, count(*) AS n,
         avg(-(c4h / entry - 1) + fund4h - {PERP_COST}) AS s4, median(-(c4h / entry - 1) + fund4h - {PERP_COST}) AS m4, avg((-(c4h / entry - 1) + fund4h > {PERP_COST})::INT) AS w4,
         avg(-(c24h / entry - 1) + fund24h - {PERP_COST}) AS s24, median(-(c24h / entry - 1) + fund24h - {PERP_COST}) AS m24, avg((-(c24h / entry - 1) + fund24h > {PERP_COST})::INT) AS w24,
         avg(fund24h) AS f24
  FROM funded {where} GROUP BY 1, 2 ORDER BY 1, 2""").fetchall()
fmt = lambda rows: table([(s, v, n, pct(a), pct(b), f"{w:.0%}", pct(c), pct(d), f"{e:.0%}", pct(f)) for s, v, n, a, b, w, c, d, e, f in rows],
                         ["rise", "volume", "n", "short 4h avg", "4h med", "4h win", "short 24h avg", "24h med", "24h win", "funding 24h"])
out = "A short opened at the next bar after the rise, net of 0.15% and with funding (positive funding pays the short):\n\n" + fmt(q(""))
out += "\n\nSince 2024 only:\n\n" + fmt(q("WHERE yr >= 2024"))
rows = con.execute(f"SELECT yr, count(*), avg(-(c24h / entry - 1) + fund24h - {PERP_COST}), median(-(c24h / entry - 1) + fund24h - {PERP_COST}) FROM funded WHERE r15 >= 0.05 GROUP BY 1 ORDER BY 1").fetchall()
out += "\n\nShort 24h after a 5%+ rise, by year:\n\n" + table([(y, n, pct(a), pct(b)) for y, n, a, b in rows], ["year", "n", "avg", "median"])
out += f"\n\n{time.time() - t0:.0f}s."
write("short_pump", "Shorting a pump on perpetuals (Binance USDT-M, 2020–2026)", out)
