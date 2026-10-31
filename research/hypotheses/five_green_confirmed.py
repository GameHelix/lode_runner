"""Five green 15-minute candles with institutional-style confirmation: volume growth, taker-buy share (the CVD proxy), open interest rising over the run, BTC up over the same window, price not extended from VWAP. Does the score separate winners from losers?"""
import os, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import *

t0 = time.time()
con = connect()
con.execute("SET memory_limit = '11GB'")
con.execute("""
  CREATE TABLE b AS
  WITH m AS (
    SELECT symbol, time_bucket(INTERVAL '15 minutes', open_time) AS t, first(open ORDER BY open_time) AS o, max(high) AS h, min(low) AS l, last(close ORDER BY open_time) AS c,
           sum(quote_volume) AS qv, sum(taker_buy_quote_volume) AS tbq, sum(volume) AS v
    FROM um_klines_1m WHERE open_time >= '2021-01-01' GROUP BY 1, 2),
  d AS (SELECT symbol, date_trunc('day', t) AS day, sum(qv) AS day_qv FROM m GROUP BY 1, 2)
  SELECT m.*, d.day_qv / 96 AS usual_qv
  FROM m JOIN d ON d.symbol = m.symbol AND d.day = date_trunc('day', m.t) - INTERVAL '1 day'
  JOIN (SELECT symbol, min(t) AS listed FROM m GROUP BY 1) l ON l.symbol = m.symbol
  WHERE m.o > 0 AND d.day_qv >= 1e6 AND m.t >= l.listed + INTERVAL '7 days'
""")
con.execute("""
  CREATE TABLE btc AS SELECT t, c, c / lag(c, 5) OVER (ORDER BY t) - 1 AS btc5 FROM b WHERE symbol = 'BTCUSDT'
""")
con.execute("""
  CREATE TABLE oi AS SELECT symbol, time_bucket(INTERVAL '15 minutes', time) AS t, last(open_interest ORDER BY time) AS oi FROM um_metrics WHERE open_interest > 0 AND time >= '2021-01-01' GROUP BY 1, 2
""")
con.execute("""
  CREATE TABLE sig AS
  WITH x AS (
    SELECT b.symbol, b.t, b.o, b.c, b.qv, b.usual_qv, b.tbq, o.oi, btc.btc5,
           (b.c > b.o AND lag(b.c > b.o, 1) OVER w AND lag(b.c > b.o, 2) OVER w AND lag(b.c > b.o, 3) OVER w AND lag(b.c > b.o, 4) OVER w) AS five,
           b.c / lag(b.o, 4) OVER w - 1 AS run,
           sum(b.qv) OVER (PARTITION BY b.symbol ORDER BY b.t ROWS BETWEEN 4 PRECEDING AND CURRENT ROW) AS qv5,
           sum(b.tbq) OVER (PARTITION BY b.symbol ORDER BY b.t ROWS BETWEEN 4 PRECEDING AND CURRENT ROW) AS tbq5,
           lag(b.qv, 4) OVER w AS qv_first,
           o.oi / lag(o.oi, 5) OVER w - 1 AS doi,
           sum(b.qv) OVER (PARTITION BY b.symbol, date_trunc('day', b.t) ORDER BY b.t) / nullif(sum(b.v) OVER (PARTITION BY b.symbol, date_trunc('day', b.t) ORDER BY b.t), 0) AS vwap,
           lead(b.o, 1) OVER w AS entry, lead(b.c, 4) OVER w AS c1h, lead(b.c, 16) OVER w AS c4h, lead(b.c, 96) OVER w AS c24h,
           max(b.h) OVER (PARTITION BY b.symbol ORDER BY b.t ROWS BETWEEN 1 FOLLOWING AND 96 FOLLOWING) AS hi24,
           min(b.l) OVER (PARTITION BY b.symbol ORDER BY b.t ROWS BETWEEN 1 FOLLOWING AND 16 FOLLOWING) AS lo4,
           b.t - lag(b.t, 5) OVER w AS gap
    FROM b LEFT JOIN oi o ON o.symbol = b.symbol AND o.t = b.t LEFT JOIN btc ON btc.t = b.t
    WINDOW w AS (PARTITION BY b.symbol ORDER BY b.t))
  SELECT *, (qv5 >= 1.5 * 5 * usual_qv AND qv > qv_first)::INT AS c_volume, (tbq5 / nullif(qv5, 0) >= 0.55)::INT AS c_buyers,
         (doi >= 0.02)::INT AS c_oi, (btc5 >= 0)::INT AS c_btc, (c / nullif(vwap, 0) - 1 <= 0.03)::INT AS c_vwap,
         least(c1h / entry - 1, 5) AS f1h, least(c4h / entry - 1, 5) AS f4h, least(c24h / entry - 1, 5) AS f24h, least(hi24 / entry - 1, 5) AS mfe24, lo4 / entry - 1 AS mae4
  FROM x WHERE five AND entry > 0 AND c24h IS NOT NULL AND gap = INTERVAL '75 minutes' AND doi IS NOT NULL AND btc5 IS NOT NULL
""")
days = con.execute("SELECT count(DISTINCT date_trunc('day', t)) FROM sig").fetchone()[0]
def row(label, where):
    r = con.execute(f"""SELECT count(*), count(*) / {days}, median(f1h), median(f4h), avg((f4h > 0)::INT), median(f24h), avg((f24h > 0)::INT), avg((mfe24 >= 0.05)::INT), avg((mfe24 >= 0.10)::INT), median(mae4)
                        FROM sig WHERE {where}""").fetchone()
    n, pd, m1, m4, u4, m24, u24, t5, t10, mae = r
    return (label, n, f"{pd:.1f}", pct(m1), pct(m4), f"{u4:.0%}", pct(m24), f"{u24:.0%}", f"{t5:.0%}", f"{t10:.0%}", pct(mae))
hdr = ["setup", "n", "/day", "1h med", "4h med", "4h up", "24h med", "24h up", "touch +5% 24h", "touch +10% 24h", "dip 4h"]
rows = [row("all five-green runs", "TRUE")]
for k in range(0, 6):
    rows.append(row(f"score {k} of 5 confirmations", f"c_volume + c_buyers + c_oi + c_btc + c_vwap = {k}"))
rows += [row("fresh capital: volume + buyers + OI up", "c_volume = 1 AND c_buyers = 1 AND c_oi = 1"),
         row("fresh capital + BTC up + not extended", "c_volume = 1 AND c_buyers = 1 AND c_oi = 1 AND c_btc = 1 AND c_vwap = 1"),
         row("short covering: run 3%+ with OI down 2%+", "run >= 0.03 AND doi <= -0.02"),
         row("run 3%+ with OI up 5%+", "run >= 0.03 AND doi >= 0.05"),
         row("run 5%+, all five confirmations", "run >= 0.05 AND c_volume + c_buyers + c_oi + c_btc + c_vwap = 5")]
out = f"Perpetuals 2021–2026, coins with $1M traded the day before, {days} days. Five completed green 15-minute candles, bought at the next open, before costs (about 0.15% round trip). Confirmations: volume (the five candles' volume 1.5x usual and rising), buyers (taker buys at least 55% of volume over the run, the CVD proxy), OI (open interest up 2%+ over the run), BTC (up over the same 75 minutes), VWAP (price within 3% of the day's VWAP).\n\n" + table(rows, hdr)
yrs = con.execute("SELECT yr, count(*), median(f24h), avg((f24h > 0)::INT) FROM (SELECT year(t) AS yr, f24h FROM sig WHERE c_volume = 1 AND c_buyers = 1 AND c_oi = 1 AND c_btc = 1 AND c_vwap = 1) GROUP BY 1 ORDER BY 1").fetchall()
out += "\n\nAll five confirmations, by year:\n\n" + table([(y, n, pct(m), f"{u:.0%}") for y, n, m, u in yrs], ["year", "n", "24h median", "24h up"])
out += f"\n\n{time.time() - t0:.0f}s."
write("five_green_confirmed", "Five green candles with institutional confirmation (USDT-M perpetuals, 2021–2026)", out)
