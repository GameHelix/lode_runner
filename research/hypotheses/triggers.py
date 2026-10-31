"""A catalogue of candidate alert triggers evaluated on the perpetuals: how often each fires, what the coin did after it, and how consistent that was across years."""
import json, os, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import *

SINCE = sys.argv[1] if len(sys.argv) > 1 else "2021-01-01"
t0 = time.time()
con = connect()
con.execute("SET memory_limit = '11GB'")
bars15(con, "um_klines_1m")
con.execute(f"DELETE FROM bars WHERE t < TIMESTAMP '{SINCE}'")
con.execute("""
  CREATE TABLE feat AS
  WITH oi AS (SELECT symbol, time_bucket(INTERVAL '15 minutes', time) AS t, last(open_interest ORDER BY time) AS oi FROM um_metrics WHERE open_interest > 0 GROUP BY 1, 2),
  fr AS (SELECT symbol, time_bucket(INTERVAL '15 minutes', funding_time) AS t, last(rate ORDER BY funding_time) AS rate FROM um_funding GROUP BY 1, 2),
  x AS (
    SELECT b.symbol, b.t, b.o, b.h, b.l, b.c, b.qv / b.usual_qv AS vr, o.oi,
           last_value(fr.rate IGNORE NULLS) OVER (PARTITION BY b.symbol ORDER BY b.t ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS rate,
           b.c / b.o - 1 AS r15, b.c / lag(b.c, 4) OVER w - 1 AS r1h, b.c / lag(b.c, 16) OVER w - 1 AS r4h, b.c / lag(b.c, 96) OVER w - 1 AS r24h,
           stddev_samp(b.c / b.o - 1) OVER (PARTITION BY b.symbol ORDER BY b.t ROWS BETWEEN 96 PRECEDING AND 1 PRECEDING) AS sigma,
           max(b.h) OVER (PARTITION BY b.symbol ORDER BY b.t ROWS BETWEEN 96 PRECEDING AND 1 PRECEDING) AS hi24,
           min(b.l) OVER (PARTITION BY b.symbol ORDER BY b.t ROWS BETWEEN 96 PRECEDING AND 1 PRECEDING) AS lo24,
           max(b.h) OVER (PARTITION BY b.symbol ORDER BY b.t ROWS BETWEEN 672 PRECEDING AND 1 PRECEDING) AS hi7d,
           o.oi / lag(o.oi, 4) OVER w - 1 AS doi1h, o.oi / lag(o.oi, 16) OVER w - 1 AS doi4h,
           lead(b.o, 1) OVER w AS entry, lead(b.c, 4) OVER w AS c1h, lead(b.c, 16) OVER w AS c4h, lead(b.c, 96) OVER w AS c24h,
           min(b.l) OVER (PARTITION BY b.symbol ORDER BY b.t ROWS BETWEEN 1 FOLLOWING AND 16 FOLLOWING) AS lo_after,
           max(b.h) OVER (PARTITION BY b.symbol ORDER BY b.t ROWS BETWEEN 1 FOLLOWING AND 16 FOLLOWING) AS hi_after,
           b.t - lag(b.t, 672) OVER w AS gap
    FROM bars b LEFT JOIN oi o ON o.symbol = b.symbol AND o.t = b.t LEFT JOIN fr ON fr.symbol = b.symbol AND fr.t = b.t
    WINDOW w AS (PARTITION BY b.symbol ORDER BY b.t))
  SELECT *, (1 + r24h) / (1 + r15) - 1 AS prior24, least(c1h / entry - 1, 5) AS f1h, least(c4h / entry - 1, 5) AS f4h, least(c24h / entry - 1, 5) AS f24h,
         lo_after / entry - 1 AS mae, least(hi_after / entry - 1, 5) AS mfe, year(t) AS yr, greatest(sigma, 0.002) AS sig
  FROM x WHERE entry > 0 AND c24h IS NOT NULL AND gap = INTERVAL '7 days'
""")
days = con.execute("SELECT count(DISTINCT date_trunc('day', t)) FROM feat").fetchone()[0]
triggers = [
    ("rise 15m 3σ on 2x volume (radar)", "r15 >= 0.03 AND r15 >= 3 * sig AND vr >= 2", 1),
    ("drop 15m 3σ on 2x volume (radar)", "r15 <= -0.03 AND -r15 >= 3 * sig AND vr >= 2", -1),
    ("hour +6% (radar)", "r1h >= 0.06", 1),
    ("hour -6% (radar)", "r1h <= -0.06", -1),
    ("rise 15m 5%+ on thin volume", "r15 >= 0.05 AND vr < 1.5", 1),
    ("crash 10%+ of unpumped coin", "r15 <= -0.10 AND prior24 < 0.2", -1),
    ("crash 10%+ after 20%+ pump", "r15 <= -0.10 AND prior24 >= 0.2", -1),
    ("rise 1h 2%+ with OI +10%", "r1h >= 0.02 AND doi1h >= 0.10", 1),
    ("drop 1h 2%+ with OI +10%", "r1h <= -0.02 AND doi1h >= 0.10", -1),
    ("drop 1h 3%+ with OI -10% (longs flushed)", "r1h <= -0.03 AND doi1h <= -0.10", -1),
    ("rise 1h 3%+ with OI -10% (shorts squeezed)", "r1h >= 0.03 AND doi1h <= -0.10", 1),
    ("OI +10% in 4h, price flat", "abs(r4h) < 0.01 AND doi4h >= 0.10", 1),
    ("volume 5x, price flat", "vr >= 5 AND abs(r15) < 0.005", 1),
    ("new 24h high on 2x volume", "c > hi24 AND vr >= 2 AND r15 >= 0.01", 1),
    ("new 7-day high on 2x volume", "c > hi7d AND vr >= 2 AND r15 >= 0.01", 1),
    ("new 24h low on 2x volume", "c < lo24 AND vr >= 2 AND r15 <= -0.01", -1),
    ("upper wick 5%+ rejected", "(h - greatest(o, c)) / o >= 0.05 AND c <= o", 1),
    ("lower wick 5%+ rejected", "(least(o, c) - l) / o >= 0.05 AND c >= o", -1),
    ("funding <= -0.2%", "rate <= -0.002", -1),
    ("funding >= 0.3%", "rate >= 0.003", 1),
    ("4h grind +8%, low vol", "r4h >= 0.08 AND sig < 0.01", 1),
    ("4h +8% with OI -10% (squeeze)", "r4h >= 0.08 AND doi4h <= -0.10", 1),
]
rows, out_json = [], {}
for name, cond, d in triggers:
    r = con.execute(f"""
      SELECT count(*), count(DISTINCT (symbol, date_trunc('hour', t))) / {days},
             median(f1h), median(f4h), avg((f4h > 0)::INT), avg((f4h * {d} >= 0.03)::INT), avg((f4h * {d} <= -0.03)::INT),
             median(f24h), avg((f24h > 0)::INT), median(CASE WHEN {d} > 0 THEN mae ELSE -mfe END),
             median(f24h) FILTER (WHERE yr >= 2025), avg((f24h > 0)::INT) FILTER (WHERE yr >= 2025)
      FROM feat WHERE {cond}""").fetchone()
    years = con.execute(f"SELECT yr, median(f24h) FROM feat WHERE {cond} GROUP BY 1").fetchall()
    n, per_day, m1, m4, u4, cont, rev, m24, u24, adverse, m24r, u24r = r
    if not n:
        continue
    sign = 1 if m24 > 0 else -1
    agree = sum(1 for _, m in years if m is not None and (m > 0) == (sign > 0)) / max(len(years), 1)
    rows.append((name, n, f"{per_day:.1f}", pct(m1), pct(m4), f"{u4:.0%}", f"{cont:.0%}", f"{rev:.0%}", pct(m24), f"{u24:.0%}", pct(adverse), pct(m24r) if m24r is not None else "n/a", f"{u24r:.0%}" if u24r is not None else "n/a", f"{agree:.0%}"))
    out_json[name] = {"n": n, "per_day": round(per_day, 2), "median_1h": round(m1, 4), "median_4h": round(m4, 4), "up_4h": round(u4, 3), "continues_4h": round(cont, 3), "reverses_4h": round(rev, 3), "median_24h": round(m24, 4), "up_24h": round(u24, 3), "adverse_4h": round(adverse, 4), "median_24h_2025on": round(m24r, 4) if m24r is not None else None, "years_agree": round(agree, 2)}
rows.sort(key=lambda r: -abs(float(r[8].rstrip("%"))))
out = f"Perpetuals since {SINCE}, coins with $1M traded the day before, {days} days. Returns are the coin's own, bought at the next bar (no costs; a round trip costs about 0.15%). 'continues' and 'reverses' are a further 3% in or against the move's direction within 4h; 'adverse' is the median move against the natural trade within 4h (long for up moves, short for down moves).\n\n"
out += table(rows, ["trigger", "n", "alerts/day", "1h med", "4h med", "4h up", "continues", "reverses", "24h med", "24h up", "adverse 4h", "24h med 2025+", "24h up 2025+", "years agree"])
out += f"\n\n{time.time() - t0:.0f}s."
write("triggers", "Candidate alert triggers on the perpetuals", out)
json.dump(out_json, open(os.path.join(os.path.dirname(__file__), "..", "results", "triggers.json"), "w"), indent=1)
