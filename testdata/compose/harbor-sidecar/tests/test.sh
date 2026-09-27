#!/bin/sh
# Grades against the sidecar itself, so the answer cannot be guessed and the
# test fails unless main can really reach api.
mkdir -p /logs/verifier
python3 - <<'PY'
import urllib.request, pathlib
want = urllib.request.urlopen("http://api:8000/secret.txt", timeout=5).read().decode()
got = pathlib.Path("/app/answer.txt").read_text() if pathlib.Path("/app/answer.txt").exists() else ""
pathlib.Path("/logs/verifier/reward.txt").write_text("1" if got == want else "0")
PY
