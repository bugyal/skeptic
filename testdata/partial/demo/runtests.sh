#!/bin/sh
cd /testbed
if [ "$(python3 -c 'import calc; print(calc.add(2,3))')" = "5" ]; then
  echo "PASSED t.py::test_add"
else
  echo "FAILED t.py::test_add - add is wrong"
fi
