"""The radar's own rules replayed on spot history: how many alerts a day, and what the coin did after each kind."""
import os, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import *

t0 = time.time()
con = connect()
bars15(con)
con.execute(f"""
  CREATE TABLE sig AS
  WITH x AS (
    SELECT symbol, t, c / o - 1 AS r15, qv / usual_qv AS vr, c / lag(c, 4) OVER w - 1 AS r1h,
           stddev_samp(c / o - 1) OVER (PARTITION BY symbol ORDER BY t ROWS BETWEEN 96 PRECEDING AND 1 PRECEDING) AS sigma,
           lead(o, 1) OVER w AS entry, lead(c, 16) OVER w AS c4h, lead(c, 96) OVER w AS c24h, t - lag(t, 96) OVER w AS gap
    FROM bars WINDOW w AS (PARTITION BY symbol ORDER BY t))
  SELECT symbol, t, r15, r1h, vr, least(c4h / entry - 1, 5) AS f4h, least(c24h / entry - 1, 5) AS f24h, year(t) AS yr,
         abs(r15) >= 0.03 AND abs(r15) >= 3 * greatest(sigma, 0.002) AND vr >= 2 AS big15, abs(r1h) >= 0.06 AS big1h
  FROM x WHERE entry > 0 AND c24h IS NOT NULL AND gap = INTERVAL '1 day'
""")
days = con.execute("SELECT count(DISTINCT date_trunc('day', t)) FROM sig").fetchone()[0]
days26 = con.execute("SELECT count(DISTINCT date_trunc('day', t)) FROM sig WHERE yr = 2026").fetchone()[0]
rows = con.execute(f"""
  SELECT rule, dir, count(*) AS n,
         count(DISTINCT (symbol, date_trunc('hour', t))) / {days} AS per_day,
         count(DISTINCT (symbol, date_trunc('hour', t))) FILTER (WHERE yr = 2026) / {days26} AS per_day26,
         median(f4h) - {SPOT_COST} AS m4, avg((f4h > {SPOT_COST})::INT) AS up4, avg((f4h >= 0.03)::INT) AS cont, avg((f4h <= -0.03)::INT) AS rev,
         median(f24h) - {SPOT_COST} AS m24, avg((f24h > {SPOT_COST})::INT) AS up24
  FROM (SELECT '15m move, 3σ, 2× volume' AS rule, CASE WHEN r15 > 0 THEN 'up' ELSE 'down' END AS dir, * FROM sig WHERE big15
        UNION ALL SELECT '1h move of 6%+', CASE WHEN r1h > 0 THEN 'up' ELSE 'down' END, * FROM sig WHERE big1h AND NOT big15)
  GROUP BY 1, 2 ORDER BY 1, 2 DESC
""").fetchall()
out = f"Alerts per day are counted once per coin and hour, as the radar's cooldown does; {days} days of history, {days26} in 2026. Forward returns are the coin's, net of 0.3% for a long; 'continues' is a further 3%+ in the move's direction within 4h, 'reverses' 3%+ against it.\n\n"
out += table([(r, d, n, f"{p:.1f}", f"{p26:.1f}", pct(m4), f"{u4:.0%}", f"{(c if d == 'up' else v):.0%}", f"{(v if d == 'up' else c):.0%}", pct(m24), f"{u24:.0%}") for r, d, n, p, p26, m4, u4, c, v, m24, u24 in rows],
             ["rule", "direction", "n", "alerts/day", "2026/day", "4h median", "4h up", "continues", "reverses", "24h median", "24h up"])
out += f"\n\n{time.time() - t0:.0f}s."
write("radar_rules", "The radar's rules replayed on spot history (2017–2026)", out)
