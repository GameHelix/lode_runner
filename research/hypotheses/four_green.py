"""After four green 15-minute candles in a row, does the coin rise 10% or more? Entry at the next candle's open."""
import os, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import *

MARKET = sys.argv[1] if len(sys.argv) > 1 else "spot"
SRC, COST, SUFFIX = ("um_klines_1m", PERP_COST, "_perps") if MARKET == "perps" else ("spot_klines_1m", SPOT_COST, "")
t0 = time.time()
con = connect()
bars15(con, SRC)
con.execute(f"""
  CREATE TABLE sig AS
  WITH x AS (
    SELECT symbol, t, c > o AS green, c / o - 1 AS r, qv / usual_qv AS vr,
           lead(o, 1) OVER w AS entry, lead(c, 4) OVER w AS c1h, lead(c, 16) OVER w AS c4h, lead(c, 96) OVER w AS c24h,
           max(h) OVER (PARTITION BY symbol ORDER BY t ROWS BETWEEN 1 FOLLOWING AND 16 FOLLOWING) AS hi4h,
           max(h) OVER (PARTITION BY symbol ORDER BY t ROWS BETWEEN 1 FOLLOWING AND 96 FOLLOWING) AS hi24h,
           min(l) OVER (PARTITION BY symbol ORDER BY t ROWS BETWEEN 1 FOLLOWING AND 96 FOLLOWING) AS lo24h,
           t - lag(t, 4) OVER w AS gap
    FROM bars WINDOW w AS (PARTITION BY symbol ORDER BY t)),
  y AS (
    SELECT *, (green AND lag(green, 1) OVER w AND lag(green, 2) OVER w AND lag(green, 3) OVER w) AS four,
           c4h / entry - 1 AS f4h, c24h / entry - 1 AS f24h, c1h / entry - 1 AS f1h, hi4h / entry - 1 AS mfe4h, hi24h / entry - 1 AS mfe24h, lo24h / entry - 1 AS mae24h,
           entry / lag(entry, 4) OVER w - 1 AS run4
    FROM x WINDOW w AS (PARTITION BY symbol ORDER BY t))
  SELECT symbol, t, year(t) AS yr, four, vr, run4, f1h, f4h, f24h, mfe4h, mfe24h, mae24h
  FROM y WHERE entry > 0 AND c24h IS NOT NULL AND gap = INTERVAL '1 hour'
""")
def row(label, where):
    r = con.execute(f"""
      SELECT count(*), avg((mfe4h >= 0.10)::INT), avg((f4h >= 0.10)::INT), avg((mfe24h >= 0.10)::INT), avg((f24h >= 0.10)::INT),
             median(f1h) - {COST}, median(f4h) - {COST}, avg((f4h > {COST})::INT), median(f24h) - {COST}, avg((mae24h <= -0.10)::INT)
      FROM sig WHERE {where}""").fetchone()
    n, t4, c4, t24, c24, m1, m4, u4, m24, d10 = r
    return (label, n, f"{t4:.1%}", f"{c4:.1%}", f"{t24:.1%}", f"{c24:.1%}", pct(m1), pct(m4), f"{u4:.0%}", pct(m24), f"{d10:.1%}")
hdr = ["case", "n", "touches +10% in 4h", "closes +10% at 4h", "touches +10% in 24h", "closes +10% at 24h", "1h median", "4h median", "4h up", "24h median", "falls 10% in 24h"]
rows = [row("4 green candles", "four"),
        row("4 green, rose 3%+ over them", "four AND run4 >= 0.03"),
        row("4 green, rose 5%+ over them", "four AND run4 >= 0.05"),
        row("4 green, last on 2x+ volume", "four AND vr >= 2"),
        row("any candle (base rate)", "TRUE")]
out = f"Four green 15-minute candles in a row, bought at the next candle's open, net of costs. Coins with $1M traded the day before.\n\n" + table(rows, hdr)
yrs = con.execute(f"SELECT yr, count(*), avg((mfe24h >= 0.10)::INT), avg((f24h >= 0.10)::INT), median(f24h) - {COST}, avg((f24h > {COST})::INT) FROM sig WHERE four GROUP BY 1 ORDER BY 1").fetchall()
out += "\n\nBy year, after 4 green candles:\n\n" + table([(y, n, f"{a:.1%}", f"{b:.1%}", pct(c), f"{d:.0%}") for y, n, a, b, c, d in yrs], ["year", "n", "touches +10% in 24h", "closes +10% at 24h", "24h median", "24h up"])
out += f"\n\n{time.time() - t0:.0f}s."
write("four_green" + SUFFIX, f"Four green 15-minute candles ({'USDT-M perpetuals, 2020–2026' if MARKET == 'perps' else 'spot, 2017–2026'})", out)
