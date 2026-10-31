"""The hype cycle at daily scale: after a +50% day, after a +100% week, and the week's top gainers against the rest."""
import os, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import *

t0 = time.time()
con = connect()
con.execute("""
  CREATE TABLE d AS
  WITH x AS (
    SELECT symbol, date_trunc('day', open_time) AS day, first(open ORDER BY open_time) AS o, last(close ORDER BY open_time) AS c, sum(quote_volume) AS qv
    FROM spot_klines_1m WHERE symbol LIKE '%USDT' GROUP BY 1, 2),
  y AS (
    SELECT *, c / lag(c, 7) OVER w - 1 AS r7, lead(o, 1) OVER w AS entry, lead(c, 1) OVER w AS c1, lead(c, 3) OVER w AS c3, lead(c, 7) OVER w AS c7, lead(c, 30) OVER w AS c30,
           sum(qv) OVER (PARTITION BY symbol ORDER BY day ROWS BETWEEN 7 PRECEDING AND 1 PRECEDING) AS qv7, row_number() OVER w AS age, day - lag(day, 7) OVER w AS gap
    FROM x WINDOW w AS (PARTITION BY symbol ORDER BY day))
  SELECT symbol, day, year(day) AS yr, c / o - 1 AS r1, r7, qv, entry, least(c1 / entry - 1, 5) AS f1, least(c3 / entry - 1, 5) AS f3, least(c7 / entry - 1, 5) AS f7, least(c30 / entry - 1, 5) AS f30
  FROM y WHERE o > 0 AND entry > 0 AND c30 IS NOT NULL AND qv7 >= 7e6 AND age > 7 AND gap = INTERVAL '7 days'
""")
def section(title, where):
    rows = con.execute(f"SELECT count(*), median(f1), median(f3), avg((f3 > {SPOT_COST})::INT), median(f7), median(f30), avg((f30 > {SPOT_COST})::INT), avg((f30 >= 0.5)::INT), avg((f30 <= -0.5)::INT) FROM d WHERE {where}").fetchone()
    n, m1, m3, u3, m7, m30, u30, dbl, half = rows
    return f"\n{title}: n={n}, next day median {pct(m1 - SPOT_COST)}, 3 days {pct(m3 - SPOT_COST)} ({u3:.0%} up), 7 days {pct(m7 - SPOT_COST)}, 30 days {pct(m30 - SPOT_COST)} ({u30:.0%} up; {dbl:.0%} gain 50%+, {half:.0%} lose 50%+)"
out = "Bought at the next day's open, net of 0.3%. Coins with $1M a day traded over the prior week.\n"
out += section("After a day of +50% or more", "r1 >= 0.5")
out += section("After a day of +20% to +50%", "r1 >= 0.2 AND r1 < 0.5")
out += section("After a week of +100% or more", "r7 >= 1.0")
out += section("After a week of −50% or worse", "r7 <= -0.5")
out += section("Any day, any coin (base rate)", "TRUE")
rows = con.execute(f"""
  WITH r AS (SELECT *, rank() OVER (PARTITION BY day ORDER BY r7 DESC) AS top, count(*) OVER (PARTITION BY day) AS n FROM d WHERE r7 IS NOT NULL)
  SELECT CASE WHEN yr <= 2020 THEN '2017-2020' WHEN yr <= 2022 THEN '2021-2022' WHEN yr <= 2024 THEN '2023-2024' ELSE '2025-2026' END AS period,
         median(f7) FILTER (WHERE top <= 10) - {SPOT_COST}, median(f30) FILTER (WHERE top <= 10) - {SPOT_COST}, avg((f30 > {SPOT_COST})::INT) FILTER (WHERE top <= 10),
         median(f7) - {SPOT_COST}, median(f30) - {SPOT_COST}, avg((f30 > {SPOT_COST})::INT)
  FROM r WHERE n >= 30 GROUP BY 1 ORDER BY 1
""").fetchall()
out += "\n\nThe week's 10 biggest gainers each day against every coin:\n\n" + table([(p, pct(a), pct(b), f"{c:.0%}", pct(d), pct(e), f"{f:.0%}") for p, a, b, c, d, e, f in rows], ["period", "top 10: 7d median", "top 10: 30d median", "top 10: 30d up", "all: 7d median", "all: 30d median", "all: 30d up"])
out += f"\n\n{time.time() - t0:.0f}s."
write("hype_cycle", "The hype cycle at daily scale (spot, 2017–2026)", out)
