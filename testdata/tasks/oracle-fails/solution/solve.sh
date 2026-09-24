#!/bin/sh
# Broken on purpose: writes 41, but the test demands 42.
echo 41 > /app/answer.txt
