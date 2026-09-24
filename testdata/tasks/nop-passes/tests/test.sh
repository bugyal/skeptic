#!/bin/sh
# Broken on purpose: full marks regardless of the workspace, so an agent that
# does nothing still scores 1.0.
echo 1 > /logs/verifier/reward.txt
