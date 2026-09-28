#!/bin/sh
# Passes only for the right answer in a container with no network interface
# but loopback: what Harbor's no-network mode gives the task.
mkdir -p /logs/verifier
ifaces=$(ls /sys/class/net | grep -v '^lo$' | wc -l)
if [ "$(cat /app/answer.txt 2>/dev/null)" = "42" ] && [ "$ifaces" -eq 0 ]; then
  echo 1 > /logs/verifier/reward.txt
else
  echo "answer=$(cat /app/answer.txt 2>/dev/null) non-loopback interfaces=$ifaces"
  echo 0 > /logs/verifier/reward.txt
fi
