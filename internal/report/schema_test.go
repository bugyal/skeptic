package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bugyal/skeptic/internal/check"
)

// TestReportMatchesSchema validates a generated report against the published
// schema. The check covers required keys, declared types and enums -- the
// parts a downstream consumer actually depends on -- without pulling in a
// JSON Schema library for one test.
func TestReportMatchesSchema(t *testing.T) {
	schemaBytes, err := os.ReadFile("../../docs/report-schema.json")
	if err != nil {
		t.Fatalf("reading schema: %v", err)
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	rep := Build(sampleResults(), "test", "/tmp/run", "1.47")
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshalling report: %v", err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("report is not valid JSON: %v", err)
	}

	if err := validate(doc, schema, "$"); err != nil {
		t.Fatalf("report does not match docs/report-schema.json: %v", err)
	}
}

// TestSchemaVersionMatchesDoc guards against bumping the constant without
// updating the published schema, which would silently break consumers.
func TestSchemaVersionMatchesDoc(t *testing.T) {
	b, err := os.ReadFile("../../docs/report-schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties struct {
			Schema struct {
				Const float64 `json:"const"`
			} `json:"schema"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	if int(schema.Properties.Schema.Const) != SchemaVersion {
		t.Fatalf("schema doc says %v, code says %d", schema.Properties.Schema.Const, SchemaVersion)
	}
}

func validate(doc, schema map[string]interface{}, path string) error {
	if req, ok := schema["required"].([]interface{}); ok {
		for _, k := range req {
			if _, present := doc[k.(string)]; !present {
				return fmt.Errorf("%s: missing required key %q", path, k)
			}
		}
	}
	props, _ := schema["properties"].(map[string]interface{})
	for key, raw := range props {
		spec, _ := raw.(map[string]interface{})
		val, present := doc[key]
		if !present {
			continue
		}
		child := path + "." + key
		if err := checkType(val, spec, child); err != nil {
			return err
		}
		if sub, ok := val.(map[string]interface{}); ok {
			if err := validate(sub, spec, child); err != nil {
				return err
			}
		}
		if arr, ok := val.([]interface{}); ok {
			items, _ := spec["items"].(map[string]interface{})
			for i, el := range arr {
				em, ok := el.(map[string]interface{})
				if !ok {
					continue
				}
				if err := validate(em, items, fmt.Sprintf("%s[%d]", child, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkType(val interface{}, spec map[string]interface{}, path string) error {
	if enum, ok := spec["enum"].([]interface{}); ok {
		found := false
		for _, e := range enum {
			if e == val {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s: value %v not in enum", path, val)
		}
	}
	var allowed []string
	switch tv := spec["type"].(type) {
	case string:
		allowed = []string{tv}
	case []interface{}:
		for _, x := range tv {
			allowed = append(allowed, x.(string))
		}
	default:
		return nil
	}
	for _, want := range allowed {
		switch want {
		case "object":
			if _, ok := val.(map[string]interface{}); ok {
				return nil
			}
		case "array":
			if _, ok := val.([]interface{}); ok {
				return nil
			}
		case "string":
			if _, ok := val.(string); ok {
				return nil
			}
		case "number":
			if _, ok := val.(float64); ok {
				return nil
			}
		case "integer":
			if f, ok := val.(float64); ok && f == float64(int(f)) {
				return nil
			}
		case "null":
			if val == nil {
				return nil
			}
		}
	}
	return fmt.Errorf("%s: value %v (%T) is not any of %v", path, val, val, allowed)
}

func sampleResults() []check.TaskResult {
	one, zero, half := 1.0, 0.0, 0.5
	sec := 2 * time.Second
	return []check.TaskResult{
		{ID: "s/clean", Format: "harbor", Dir: "/t/clean", Verdict: check.VerdictClean,
			Reason:   "oracle 1.00, nop 0.00",
			Nop:      &check.ControlResult{Control: check.ControlNop, Score: &zero, Duration: sec},
			Oracle:   &check.ControlResult{Control: check.ControlOracle, Score: &one, Duration: sec},
			Duration: sec, ImageDigest: "sha256:abc", LogDir: "/tmp/run/clean"},
		{ID: "s/nop", Format: "harbor", Dir: "/t/nop", Verdict: check.VerdictNopPasses,
			Reason:   "tests pass without any change (nop 1.00)",
			Nop:      &check.ControlResult{Control: check.ControlNop, Score: &one, Duration: sec},
			Oracle:   &check.ControlResult{Control: check.ControlOracle, Score: &one, Duration: sec},
			Duration: sec},
		{ID: "s/both", Format: "harbor", Dir: "/t/both", Verdict: check.VerdictBoth,
			Reason:   "nop 0.50, oracle 0.50",
			Nop:      &check.ControlResult{Control: check.ControlNop, Score: &half, Duration: sec},
			Oracle:   &check.ControlResult{Control: check.ControlOracle, Score: &half, Duration: sec},
			Duration: sec},
		{ID: "s/err", Format: "harbor", Dir: "/t/err", Verdict: check.VerdictError,
			Reason: "build failed", Error: "build failed", Duration: sec},
		{ID: "s/flaky", Format: "harbor", Dir: "/t/flaky", Verdict: check.VerdictFlaky,
			Reason: "scores differ between identical runs: nop 0.00, 0.00; oracle 1.00, 0.00",
			Nop:    &check.ControlResult{Control: check.ControlNop, Score: &zero, Duration: sec},
			Oracle: &check.ControlResult{Control: check.ControlOracle, Score: &one, Duration: sec},
			NopRuns: []*check.ControlResult{
				{Control: check.ControlNop, Score: &zero}, {Control: check.ControlNop, Score: &zero}},
			OracleRuns: []*check.ControlResult{
				{Control: check.ControlOracle, Score: &one}, {Control: check.ControlOracle, Score: &zero}},
			Duration: sec},
		{ID: "s/noorc", Format: "harbor", Dir: "/t/noorc", Verdict: check.VerdictNoOracle,
			Reason:   "no reference solution; oracle not run",
			Nop:      &check.ControlResult{Control: check.ControlNop, Score: &zero, Duration: sec},
			Duration: sec},
	}
}

// A long sweep writes one report per instance as it finishes. Loading the
// directory must merge them, so a run that is still going -- or was
// interrupted -- is readable as a partial result instead of useless.
func TestLoadMergesFragments(t *testing.T) {
	dir := t.TempDir()
	for i, res := range sampleResults() {
		part := Build([]check.TaskResult{res}, "test", dir, "1.47")
		if err := part.WriteJSON(filepath.Join(dir, fmt.Sprintf("frag-%d.json", i))); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := len(sampleResults())
	if got.Totals.Total != want {
		t.Fatalf("Total = %d, want %d", got.Totals.Total, want)
	}
	// Totals must be recomputed across fragments, not taken from one of them.
	if got.Totals.Clean != 1 || got.Totals.Flagged != 3 || got.Totals.Errors != 1 || got.Totals.NoOracle != 1 {
		t.Errorf("totals = %+v, want 1 clean / 3 flagged / 1 error / 1 no-oracle", got.Totals)
	}
	// Flagged tasks sort first so a partial sweep leads with what matters.
	if check.Verdict(got.Tasks[0].Verdict) == check.VerdictClean {
		t.Errorf("first task is CLEAN; flagged tasks should sort first")
	}
}

// A duplicate instance across fragments must be counted once.
func TestLoadMergeDeduplicates(t *testing.T) {
	dir := t.TempDir()
	res := sampleResults()[0]
	for i := 0; i < 3; i++ {
		part := Build([]check.TaskResult{res}, "test", dir, "1.47")
		if err := part.WriteJSON(filepath.Join(dir, fmt.Sprintf("dup-%d.json", i))); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Totals.Total != 1 {
		t.Fatalf("Total = %d, want 1 after deduplication", got.Totals.Total)
	}
}

// A directory holding report.json keeps loading exactly that.
func TestLoadPrefersReportJSON(t *testing.T) {
	dir := t.TempDir()
	full := Build(sampleResults(), "test", dir, "1.47")
	if err := full.WriteJSON(filepath.Join(dir, "report.json")); err != nil {
		t.Fatal(err)
	}
	// A stray fragment must not be merged in on top of it.
	stray := Build(sampleResults()[:1], "test", dir, "1.47")
	if err := stray.WriteJSON(filepath.Join(dir, "stray.json")); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Totals.Total != len(sampleResults()) {
		t.Fatalf("Total = %d, want the report.json totals (%d)", got.Totals.Total, len(sampleResults()))
	}
}

func TestLoadEmptyDir(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("expected an error for a directory with no reports")
	}
}

// Reports written before --repeat existed carry schema 1. They must still
// load: every run under results/ is one of them.
func TestSchemaOneStillLoads(t *testing.T) {
	p := filepath.Join(t.TempDir(), "report.json")
	old := `{"schema": 1, "skeptic_version": "0.1.1", "generated_at": "2026-09-25T00:00:00Z",
		"host": {"os": "darwin", "arch": "arm64"}, "run_dir": "x",
		"totals": {"total": 1, "clean": 1, "flagged": 0, "errors": 0, "no_oracle": 0, "unsupported": 0},
		"tasks": [{"id": "a", "format": "swebench", "dir": "d", "verdict": "CLEAN", "reason": "",
			"nop_score": 0, "oracle_score": 1, "seconds": 1}]}`
	if err := os.WriteFile(p, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Load(p)
	if err != nil {
		t.Fatalf("schema 1 report did not load: %v", err)
	}
	if r.Schema != 1 || len(r.Tasks) != 1 || r.Tasks[0].Verdict != "CLEAN" {
		t.Errorf("loaded %+v", r)
	}

	future := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(future, []byte(`{"schema": 99, "tasks": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(future); err == nil {
		t.Error("a schema newer than this build must be refused, not half-read")
	}
}

func TestScoresShowRangeOnlyWhenRunsDisagree(t *testing.T) {
	zero, one := 0.0, 1.0
	if got := scores(&one, []*float64{&one, &zero, &one}); got != "0.00–1.00" {
		t.Errorf("disagreeing runs = %q, want the range", got)
	}
	if got := scores(&one, []*float64{&one, &one}); got != "1.00" {
		t.Errorf("agreeing runs = %q, want 1.00", got)
	}
	if got := scores(&zero, nil); got != "0.00" {
		t.Errorf("single run = %q, want 0.00", got)
	}
}
