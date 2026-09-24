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
