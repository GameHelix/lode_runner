import os, sys, tempfile, time
import duckdb

# The archive as published on Hugging Face; BINANCE_DATA points at a local
# copy of it instead, a folder with the four sets.
DATA = os.environ.get("BINANCE_DATA", "hf://datasets/ismatsamadov/binance-usdt-archive")
SPOT_COST, PERP_COST = 0.003, 0.0015

def connect():
    con = duckdb.connect()
    con.execute("SET threads = 8")
    con.execute("SET memory_limit = '10GB'")
    con.execute(f"SET temp_directory = '{os.path.join(tempfile.gettempdir(), 'duckdb')}'")
    for name in ("spot_klines_1m", "um_klines_1m", "um_funding", "um_metrics"):
        con.execute(f"CREATE VIEW {name} AS SELECT * FROM read_parquet('{DATA}/{name}/*.parquet')")
    return con

def bars15(con, source="spot_klines_1m"):
    """15-minute bars with the previous day's quote volume per bar, so a bar's volume can be read against the coin's usual. A coin's first week is left out: listing candles carry placeholder prices."""
    con.execute(f"""
      CREATE TABLE bars AS
      WITH b AS (
        SELECT symbol, time_bucket(INTERVAL '15 minutes', open_time) AS t,
               first(open ORDER BY open_time) AS o, max(high) AS h, min(low) AS l, last(close ORDER BY open_time) AS c,
               sum(quote_volume) AS qv
        FROM {source} WHERE symbol LIKE '%USDT' GROUP BY 1, 2),
      d AS (SELECT symbol, date_trunc('day', t) AS day, sum(qv) AS day_qv FROM b GROUP BY 1, 2)
      SELECT b.*, d.day_qv / 96 AS usual_qv
      FROM b JOIN d ON d.symbol = b.symbol AND d.day = date_trunc('day', b.t) - INTERVAL '1 day'
      JOIN (SELECT symbol, min(t) AS listed FROM b GROUP BY 1) l ON l.symbol = b.symbol
      WHERE b.o > 0 AND d.day_qv >= 1e6 AND b.t >= l.listed + INTERVAL '7 days'
    """)

def table(rows, header):
    widths = [max(len(str(x)) for x in col) for col in zip(header, *rows)]
    line = lambda r: "| " + " | ".join(str(x).ljust(w) for x, w in zip(r, widths)) + " |"
    return "\n".join([line(header), "|" + "|".join("-" * (w + 2) for w in widths) + "|"] + [line(r) for r in rows])

def write(name, title, text):
    path = os.path.join(os.path.dirname(__file__), "..", "results", name + ".md")
    with open(path, "w") as f:
        f.write(f"# {title}\n\n{text}\n")
    print(text)
    print(f"-> results/{name}.md")

def pct(x):
    return f"{x * 100:+.1f}%" if x == x else "n/a"
