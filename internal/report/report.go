// Package report renders check results as JSON, a table, or markdown.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/bugyal/skeptic/internal/check"
)

// SchemaVersion is the version of the on-disk report format. It is bumped only
// for changes that would break a consumer reading an older report.
const SchemaVersion = 1

// Report is the full, versioned result of one run.
type Report struct {
	Schema         int          `json:"schema"`
	SkepticVersion string       `json:"skeptic_version"`
	GeneratedAt    time.Time    `json:"generated_at"`
	Host           Host         `json:"host"`
	RunDir         string       `json:"run_dir"`
	Totals         Totals       `json:"totals"`
	Tasks          []TaskReport `json:"tasks"`
}

// Host records where the run happened, since verdicts can depend on it.
type Host struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	DockerAPI string `json:"docker_api,omitempty"`
}

// Totals summarises the run. Errors, no-oracle and unsupported tasks are
// counted separately so none of them can be mistaken for a pass or a failure.
type Totals struct {
	Total       int `json:"total"`
	Clean       int `json:"clean"`
	Flagged     int `json:"flagged"`
	Errors      int `json:"errors"`
	NoOracle    int `json:"no_oracle"`
	Unsupported int `json:"unsupported"`
	// WeakTests counts tasks with at least one unnoticed hunk. Advisory, and
	// deliberately excluded from Flagged.
	WeakTests int `json:"weak_tests"`
}

// TaskReport is one task's entry in the report.
type TaskReport struct {
	ID          string   `json:"id"`
	Format      string   `json:"format"`
	Dir         string   `json:"dir"`
	Verdict     string   `json:"verdict"`
	Reason      string   `json:"reason"`
	NopScore    *float64 `json:"nop_score"`
	OracleScore *float64 `json:"oracle_score"`
	NopSeconds  float64  `json:"nop_seconds"`
	OrcSeconds  float64  `json:"oracle_seconds"`
	Seconds     float64  `json:"seconds"`
	ImageDigest string   `json:"image_digest,omitempty"`
	LogDir      string   `json:"log_dir,omitempty"`
	Error       string   `json:"error,omitempty"`
	// WeakTests names behaviour-changing hunks whose absence the test suite
	// did not notice. Advisory: it never flags a task on its own.
	WeakTests []string `json:"weak_tests,omitempty"`
	// UngradedCleanup names ungraded deletion-only hunks, usually dead-code
	// removal that no test could observe. Kept apart from WeakTests so it
	// does not read as a finding.
	UngradedCleanup []string `json:"ungraded_cleanup,omitempty"`
}

// Build assembles a Report from raw results.
func Build(results []check.TaskResult, version, runDir, dockerAPI string) Report {
	r := Report{
		Schema:         SchemaVersion,
		SkepticVersion: version,
		GeneratedAt:    time.Now().UTC(),
		Host:           Host{OS: runtime.GOOS, Arch: runtime.GOARCH, DockerAPI: dockerAPI},
		RunDir:         runDir,
	}
	for _, res := range results {
		tr := TaskReport{
			ID: res.ID, Format: res.Format, Dir: res.Dir,
			Verdict: string(res.Verdict), Reason: res.Reason,
			NopScore: res.NopScore(), OracleScore: res.OracleScore(),
			Seconds:     res.Duration.Seconds(),
			ImageDigest: res.ImageDigest, LogDir: res.LogDir, Error: res.Error,
			WeakTests: res.WeakTests, UngradedCleanup: res.UngradedCleanup,
		}
		if res.Nop != nil {
			tr.NopSeconds = res.Nop.Duration.Seconds()
		}
		if res.Oracle != nil {
			tr.OrcSeconds = res.Oracle.Duration.Seconds()
		}
		r.Tasks = append(r.Tasks, tr)

		r.Totals.Total++
		if len(res.WeakTests) > 0 {
			r.Totals.WeakTests++
		}
		switch res.Verdict {
		case check.VerdictClean:
			r.Totals.Clean++
		case check.VerdictError:
			r.Totals.Errors++
		case check.VerdictNoOracle:
			r.Totals.NoOracle++
		case check.VerdictUnsupported:
			r.Totals.Unsupported++
		default:
			r.Totals.Flagged++
		}
	}
	sortFlaggedFirst(r.Tasks)
	return r
}

// rank orders verdicts so the things a reader must act on come first.
func rank(v string) int {
	switch check.Verdict(v) {
	case check.VerdictBoth:
		return 0
	case check.VerdictNopPasses:
		return 1
	case check.VerdictOracleFails:
		return 2
	case check.VerdictError:
		return 3
	case check.VerdictNoOracle:
		return 4
	case check.VerdictUnsupported:
		return 5
	default:
		return 6
	}
}

func sortFlaggedFirst(ts []TaskReport) {
	sort.SliceStable(ts, func(i, j int) bool {
		ri, rj := rank(ts[i].Verdict), rank(ts[j].Verdict)
		if ri != rj {
			return ri < rj
		}
		return ts[i].ID < ts[j].ID
	})
}

// AnyFlagged reports whether the run found something that should fail CI.
func (r Report) AnyFlagged() bool { return r.Totals.Flagged > 0 || r.Totals.Errors > 0 }

// WriteJSON writes the report to a file.
func (r Report) WriteJSON(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Load reads a report from a JSON file, or from a run directory.
//
// A directory holding report.json loads that. A directory holding many
// per-task reports instead merges them, which is what a long sweep produces:
// one file per instance, written as each finishes. Merging means a sweep that
// is still running, or was interrupted, is still readable as a partial result
// rather than being useless until the end.
func Load(path string) (Report, error) {
	var r Report
	info, err := os.Stat(path)
	if err != nil {
		return r, err
	}

	if info.IsDir() {
		single := filepath.Join(path, "report.json")
		if _, err := os.Stat(single); err == nil {
			return loadFile(single)
		}
		return loadDir(path)
	}
	return loadFile(path)
}

func loadFile(path string) (Report, error) {
	var r Report
	b, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("parsing %s: %w", path, err)
	}
	// Schema 0 means the file carried tasks without a full report envelope,
	// which is how per-task fragments are written during a sweep.
	if r.Schema != 0 && r.Schema != SchemaVersion {
		return r, fmt.Errorf("report schema %d is not supported (this build reads schema %d)",
			r.Schema, SchemaVersion)
	}
	return r, nil
}

// loadDir merges every report fragment in a directory into one report,
// recomputing the totals so a partial sweep still tallies correctly.
func loadDir(dir string) (Report, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Report{}, err
	}

	merged := Report{
		Schema:      SchemaVersion,
		GeneratedAt: time.Now().UTC(),
		Host:        Host{OS: runtime.GOOS, Arch: runtime.GOARCH},
		RunDir:      dir,
	}
	seen := map[string]bool{}
	var parsed int

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		part, err := loadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			// One unreadable fragment must not sink the whole partial result.
			continue
		}
		parsed++
		if merged.SkepticVersion == "" {
			merged.SkepticVersion = part.SkepticVersion
		}
		if merged.Host.DockerAPI == "" {
			merged.Host.DockerAPI = part.Host.DockerAPI
		}
		for _, t := range part.Tasks {
			if seen[t.ID] {
				continue
			}
			seen[t.ID] = true
			merged.Tasks = append(merged.Tasks, t)
		}
	}

	if parsed == 0 {
		return merged, fmt.Errorf("no report files found in %s", dir)
	}

	merged.Totals = tally(merged.Tasks)
	sortFlaggedFirst(merged.Tasks)
	return merged, nil
}

// tally recomputes totals from a task list.
func tally(ts []TaskReport) Totals {
	var t Totals
	for _, x := range ts {
		t.Total++
		if len(x.WeakTests) > 0 {
			t.WeakTests++
		}
		switch check.Verdict(x.Verdict) {
		case check.VerdictClean:
			t.Clean++
		case check.VerdictError:
			t.Errors++
		case check.VerdictNoOracle:
			t.NoOracle++
		case check.VerdictUnsupported:
			t.Unsupported++
		default:
			t.Flagged++
		}
	}
	return t
}
