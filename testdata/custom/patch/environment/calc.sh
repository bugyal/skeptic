add() {
  echo $(( $1 - $2 ))
}

# Unrelated padding so the two functions land in separate hunks.
# one
# two
# three
# four
# five
# six
# seven

mul() {
  echo $(( $1 + $2 ))
}
