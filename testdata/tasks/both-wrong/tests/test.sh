#!/bin/sh
# Broken twice over: always awards 0.5, so nop scores above zero and the
# oracle never reaches 1.0.
echo 0.5 > /logs/verifier/reward.txt
