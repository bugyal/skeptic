#!/bin/sh
if [ -s /app/out.txt ] && [ "$(cat /app/out.txt)" = "$(uname -s)" ]; then
  echo 1 > /logs/verifier/reward.txt
else
  echo 0 > /logs/verifier/reward.txt
fi
