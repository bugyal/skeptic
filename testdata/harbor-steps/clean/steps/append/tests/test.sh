#!/bin/sh
. /tests/helpers.sh
# Harbor clears the previous step's tests and verifier output before this
# step; a run that did not would leave both behind.
[ -e /tests/stale.txt ] && fail "the previous step's tests are still in /tests"
[ "$EXPECTED" = "one two" ] || fail "step verifier env not set: EXPECTED=$EXPECTED"
[ "$(cat /app/log.txt 2>/dev/null | tr '\n' ' ' | sed 's/ $//')" = "$EXPECTED" ] && pass
fail "log.txt is not \"$EXPECTED\""
