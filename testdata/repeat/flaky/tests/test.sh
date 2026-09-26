#!/bin/sh
# Correct answer required, and then a coin flip: the flake.
[ "$(cat /app/answer.txt 2>/dev/null)" = "42" ] || exit 1
[ $(( $(od -An -N1 -tu1 /dev/urandom) % 2 )) -eq 0 ]
