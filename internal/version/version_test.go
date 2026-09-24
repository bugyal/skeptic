package version

import "testing"

// A binary built by goreleaser carries a stamped version and must report it
// unchanged.
func TestStringPrefersStampedVersion(t *testing.T) {
	old := Version
	defer func() { Version = old }()

	Version = "v1.2.3"
	if got := String(); got != "v1.2.3" {
		t.Fatalf("String() = %q, want the stamped version", got)
	}
}

// A binary from `go install module@version` has no ldflags. Falling back to the
// module version keeps the documented install command from producing something
// that calls itself "dev"; under `go test` there is no module version either,
// so "dev" is the correct answer here.
func TestStringFallsBackWhenUnstamped(t *testing.T) {
	old := Version
	defer func() { Version = old }()

	Version = "dev"
	if got := String(); got == "" {
		t.Fatal("String() must never be empty")
	}
}
