#!/bin/sh
. /src/calc.sh
[ "$(add 2 3)" = 5 ] || { echo "add wrong"; exit 1; }
[ "$(mul 2 3)" = 6 ] || { echo "mul wrong"; exit 1; }
echo ok
