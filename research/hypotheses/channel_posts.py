"""Pull every post of a public Telegram channel from its t.me/s preview into posts.json (id, time, text), paging back to the first; the input for live_check.py."""
import html, json, re, sys, time, urllib.request
chan = sys.argv[1]
posts = {}
before = None
for page in range(40):
    url = f"https://t.me/s/{chan}" + (f"?before={before}" if before else "")
    req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
    body = urllib.request.urlopen(req, timeout=30).read().decode()
    msgs = []
    for chunk in body.split('tgme_widget_message_wrap')[1:]:
        mid = re.search(r'data-post="%s/(\d+)"' % chan, chunk)
        text = re.search(r'<div class="tgme_widget_message_text[^"]*"[^>]*>(.*?)</div>', chunk, re.S)
        ts = re.search(r'<time datetime="([^"]+)"', chunk)
        if mid and ts:
            msgs.append((mid.group(1), text.group(1) if text else "", ts.group(1)))
    if not msgs:
        break
    for mid, text, ts in msgs:
        text = re.sub(r"<br\s*/?>", "\n", text); text = re.sub(r"<[^>]+>", "", text)
        posts[int(mid)] = {"id": int(mid), "at": ts, "text": html.unescape(text).strip()}
    before = min(int(m[0]) for m in msgs)
    if before <= 1:
        break
    time.sleep(0.5)
out = sorted(posts.values(), key=lambda p: p["id"])
json.dump(out, open("posts.json", "w"), ensure_ascii=False, indent=1)
print(len(out), "posts", out[0]["at"] if out else "", "->", out[-1]["at"] if out else "")
