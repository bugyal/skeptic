package report

import (
	"encoding/json"
	"fmt"
	"os"
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
		{ID: "s/noorc", Format: "harbor", Dir: "/t/noorc", Verdict: check.VerdictNoOracle,
			Reason:   "no reference solution; oracle not run",
			Nop:      &check.ControlResult{Control: check.ControlNop, Score: &zero, Duration: sec},
			Duration: sec},
	}
}
