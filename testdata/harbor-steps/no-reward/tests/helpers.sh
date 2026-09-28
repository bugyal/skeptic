# Shared by every step: laid under each step's own tests/.
mkdir -p /logs/verifier
pass() { echo 1 > /logs/verifier/reward.txt; exit 0; }
fail() { echo "$1"; echo 0 > /logs/verifier/reward.txt; exit 0; }
