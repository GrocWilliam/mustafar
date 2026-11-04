#!/usr/bin/env bash
# Benchmark the forge on a spec: build it in a fresh project, wait for the end, append one result line.
# Run it before and after changing prompts, skills or the orchestrator, on the same spec, and compare the lines.
# Usage: ./bench.sh [SPEC_FILE] [TIMEOUT_HOURS]   (default: examples/url-shortener/spec.md, 4 hours)
# Results: bench-results.tsv (spend, agent runs, retries, outcome, duration, forge commit).
set -euo pipefail
cd "$(dirname "$0")"

spec=${1:-examples/url-shortener/spec.md}
hours=${2:-4}
name="bench-$(basename "$spec" .md)-$(date +%Y%m%d-%H%M%S)"
commit="$(git rev-parse --short HEAD)$(git diff --quiet HEAD -- agents orchestrator templates || echo '+dirty')"

url=$(./new-project.sh "$name" "$spec" | sed -n 's/^spec opened: //p')
[ -n "$url" ] || { echo "the spec was not opened" >&2; exit 1; }
echo "benchmarking $url (forge $commit), up to ${hours}h"

FORGE_PASSWORD=$(cat secrets/gitea-admin-password) python3 -I - "$name" "${url##*/}" "$hours" "$commit" "$spec" <<'EOF'
import base64, json, os, re, sys, time, urllib.request

name, number, hours, commit, spec = sys.argv[1], int(sys.argv[2]), float(sys.argv[3]), sys.argv[4], sys.argv[5]
auth = "Basic " + base64.b64encode(b"forge:" + os.environ["FORGE_PASSWORD"].strip().encode()).decode()

def api(path):
    req = urllib.request.Request(f"http://gitea:3000/api/v1/repos/forge/{name}{path}", headers={"Authorization": auth})
    with urllib.request.urlopen(req) as resp:
        return json.load(resp)

def labels(issue):
    return {l["name"] for l in issue["labels"]}

def stories(spec_comments):
    for body in reversed(spec_comments):
        if m := re.search(r"<!-- forge:stories ([\d,]+) -->", body):
            return [int(n) for n in m.group(1).split(",")]
    return []

def finished():
    """The outcome once nothing more will happen without a human, else None."""
    spec_issue = api(f"/issues/{number}")
    if spec_issue["state"] == "closed":
        return "done"
    if "us-blocked" in labels(spec_issue):
        return "spec blocked"
    numbers = stories([c["body"] for c in api(f"/issues/{number}/comments")])
    if not numbers:
        return None
    issues = [api(f"/issues/{n}") for n in numbers]
    # ponytail: a story waiting on a blocked one stays us-todo forever, so "all left are blocked or wait on blocked" would be exact; stopping when nothing runs and some are blocked is close enough.
    active = [i for i in issues if i["state"] == "open" and labels(i) & {"us-in-progress", "us-review"}]
    if any("us-blocked" in labels(i) for i in issues) and not active:
        return "stories blocked"
    return None

start, outcome = time.time(), None
while time.time() - start < hours * 3600:
    if outcome := finished():
        break
    time.sleep(60)
outcome = outcome or "timeout"
minutes = round((time.time() - start) / 60)

spec_comments = [c["body"] for c in api(f"/issues/{number}/comments")]
numbers = stories(spec_comments)
all_comments = spec_comments + [c["body"] for n in numbers for c in api(f"/issues/{n}/comments")]
cost = sum(float(m) for body in all_comments for m in re.findall(r"<!-- forge:cost=([\d.]+) -->", body))
runs = {}
for body in all_comments:
    if m := re.match(r"🤖 Agent `(\w+)`: ", body):
        runs[m.group(1)] = runs.get(m.group(1), 0) + 1
retries = sum(body.startswith("🔁 Attempt") for body in all_comments)
issues = [api(f"/issues/{n}") for n in numbers]
merged = sum(i["state"] == "closed" for i in issues)
blocked = sum("us-blocked" in labels(i) for i in issues)
questions = sum("❓" in body for body in spec_comments)

row = {
    "date": time.strftime("%Y-%m-%d %H:%M"), "forge": commit, "spec": spec, "project": name, "outcome": outcome,
    "minutes": minutes, "cost_usd": f"{cost:.2f}", "stories": len(numbers), "merged": merged, "blocked": blocked,
    "retries": retries, "questions": questions,
    "runs": " ".join(f"{role}={n}" for role, n in sorted(runs.items())),
}
path = "bench-results.tsv"
new = not os.path.exists(path)
with open(path, "a", encoding="utf-8") as out:
    if new:
        out.write("\t".join(row) + "\n")
    out.write("\t".join(str(v) for v in row.values()) + "\n")
for key, value in row.items():
    print(f"{key:>10}: {value}")
EOF
