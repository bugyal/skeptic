#!/bin/sh
# Terminal-Bench grades by this script's exit status. Upstream starts the
# stack without waiting on the sidecar, so retry briefly before judging.
exec python3 "$TEST_DIR/check.py"
