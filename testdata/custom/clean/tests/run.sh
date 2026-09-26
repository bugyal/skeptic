#!/bin/sh
# Honest grading: the reward depends on what the agent actually produced.
mkdir -p /logs/verifier
if [ "$(cat /app/answer.txt 2>/dev/null)" = "42" ]; then
  echo 1 > /logs/verifier/reward.txt
else
  echo 0 > /logs/verifier/reward.txt
fi
