// Package version carries build identity, stamped by the linker at release.
package version

import "runtime/debug"

var (
	// Version is the release version, set via -ldflags at build time.
	Version = "dev"
	// Commit is the git revision, set via -ldflags at build time.
	Commit = ""
	// Date is the build date, set via -ldflags at build time.
	Date = ""
)

// String returns the release version. A binary built by goreleaser has it
// stamped via ldflags; one produced by `go install module@v0.1.0` does not, so
// fall back to the module version the toolchain records. Otherwise the
// documented install command yields a binary that calls itself "dev".
func String() string {
	if Version != "dev" && Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return Version
}

// Revision returns the commit, falling back to VCS data Go embeds in the
// binary so a `go install` build still reports something useful.
func Revision() string {
	if Commit != "" {
		return Commit
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) > 12 {
				return s.Value[:12]
			}
			return s.Value
		}
	}
	return "unknown"
}

// GoVersion returns the toolchain that built this binary.
func GoVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.GoVersion
	}
	return "unknown"
}
