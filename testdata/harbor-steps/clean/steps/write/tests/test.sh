#!/bin/sh
. /tests/helpers.sh
[ "$(cat /app/log.txt 2>/dev/null)" = "one" ] && pass
fail "log.txt is not \"one\""
