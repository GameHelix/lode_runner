"""Pull every alert the engine has kept into alerts.json (id, time, text), the shape channel_posts.py writes, for live_check.py. It asks the engine on the server over SSH with the engine's token read there, so the token never leaves the server: python3 -I hypotheses/engine_alerts.py deploy@SERVER"""
import json, subprocess, sys
host = sys.argv[1]
remote = 'T=$(sed -n "s/^ENGINE_API_TOKEN=//p" /srv/secrets/candylario.env); curl -sf -H "Authorization: Bearer $T" "http://127.0.0.1:8080/v1/radar/alerts?after=%d&limit=500"'
alerts, after = [], 0
while True:
    page = json.loads(subprocess.run(["ssh", host, remote % after], capture_output=True, text=True, check=True).stdout)["alerts"]
    if not page:
        break
    alerts += [{"id": a["seq"], "at": a["at"][:19] + "+00:00", "text": a["text"]} for a in page]
    after = page[-1]["seq"]
json.dump(alerts, open("alerts.json", "w"), ensure_ascii=False, indent=1)
print(len(alerts), "alerts", alerts[0]["at"] if alerts else "", "->", alerts[-1]["at"] if alerts else "")
