package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func run(verdicts map[string]string) Report {
	r := Report{Schema: SchemaVersion}
	for id, v := range verdicts {
		r.Tasks = append(r.Tasks, TaskReport{ID: id, Verdict: v})
	}
	return r
}

func ids(cs []Change) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func TestCompare(t *testing.T) {
	old := run(map[string]string{
		"steady":         "CLEAN",
		"nop-regress":    "CLEAN",
		"oracle-regress": "NO_ORACLE",
		"starts-flaking": "CLEAN",
		"gains-oracle":   "NOP_PASSES",
		"swaps-mode":     "NOP_PASSES",
		"fixed":          "ORACLE_FAILS",
		"partly-fixed":   "BOTH",
		"loses-oracle":   "CLEAN",
		"disk-full":      "CLEAN",
		"back-from-err":  "ERROR",
		"now-supported":  "UNSUPPORTED",
		"gone":           "CLEAN",
	})
	cur := run(map[string]string{
		"steady":         "CLEAN",
		"nop-regress":    "NOP_PASSES",
		"oracle-regress": "ORACLE_FAILS",
		"starts-flaking": "FLAKY",
		"gains-oracle":   "BOTH",
		"swaps-mode":     "ORACLE_FAILS",
		"fixed":          "CLEAN",
		"partly-fixed":   "NOP_PASSES",
		"loses-oracle":   "NO_ORACLE",
		"disk-full":      "ERROR",
		"back-from-err":  "ORACLE_FAILS",
		"now-supported":  "BOTH",
		"new":            "NOP_PASSES",
	})
	d := Compare(old, cur, "old", "new")

	for _, tc := range []struct {
		name string
		got  []Change
		want []string
	}{
		// Anything that gains a failure mode, including a swap that loses
		// one and gains another.
		{"regressed", d.Regressed, []string{"gains-oracle", "nop-regress", "oracle-regress", "starts-flaking", "swaps-mode"}},
		{"fixed", d.Fixed, []string{"fixed", "partly-fixed"}},
		{"changed", d.Changed, []string{"loses-oracle"}},
		// The roadmap's trap: an ERROR on either side is the machine until
		// shown otherwise, so neither direction is a regression -- not even
		// a task that comes back from ERROR flagged, since there is no
		// earlier verdict it regressed from.
		{"could not compare", d.Uncomparable, []string{"back-from-err", "disk-full", "now-supported"}},
		{"added", d.Added, []string{"new"}},
		{"removed", d.Removed, []string{"gone"}},
	} {
		if got := ids(tc.got); !equal(got, tc.want) {
			t.Errorf("%s = %v, want %v", tc.name, got, tc.want)
		}
	}
	if d.Unchanged != 1 {
		t.Errorf("Unchanged = %d, want 1", d.Unchanged)
	}
	if !d.HasRegressions() {
		t.Error("HasRegressions = false")
	}
}

// A run where every task errored -- a full disk -- must not exit non-zero as
// if the benchmark had regressed, and must not read as clean either: it all
// lands in "could not compare".
func TestAllErrorsIsNotARegression(t *testing.T) {
	d := Compare(run(map[string]string{"a": "CLEAN", "b": "NOP_PASSES"}),
		run(map[string]string{"a": "ERROR", "b": "ERROR"}), "old", "new")
	if d.HasRegressions() || len(d.Fixed) != 0 || len(d.Uncomparable) != 2 {
		t.Errorf("regressed %v, fixed %v, uncomparable %v", ids(d.Regressed), ids(d.Fixed), ids(d.Uncomparable))
	}
}

// Merged fragments must report the host and time they were written on, not
// the machine and moment they were read.
func TestLoadDirKeepsFragmentHost(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	for i, id := range []string{"a", "b"} {
		r := Report{Schema: 1, SkepticVersion: "0.1.1", GeneratedAt: when.Add(time.Duration(i) * time.Hour),
			Host: Host{OS: "darwin", Arch: "arm64"}, Tasks: []TaskReport{{ID: id, Verdict: "CLEAN"}}}
		b, _ := json.Marshal(r)
		if err := os.WriteFile(filepath.Join(dir, id+".json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host.OS != "darwin" || got.Host.Arch != "arm64" {
		t.Errorf("Host = %+v, want the fragments' darwin/arm64", got.Host)
	}
	if !got.GeneratedAt.Equal(when.Add(time.Hour)) {
		t.Errorf("GeneratedAt = %v, want the latest fragment's", got.GeneratedAt)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
