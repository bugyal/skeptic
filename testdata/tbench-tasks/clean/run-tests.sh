#!/bin/sh
# Terminal-Bench 1.x grading entrypoint: propagate pytest's exit status.
set -e
pytest "$TEST_DIR" -q
