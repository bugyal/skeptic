package swebench

import "testing"

func TestParsePytest(t *testing.T) {
	log := `
PASSED tests/test_a.py::test_one
FAILED tests/test_b.py::test_two - AssertionError: nope
SKIPPED [3] tests/test_c.py:12: needs network
SKIPPED tests/test_d.py::test_four
ERROR tests/test_e.py::test_five
some unrelated line
`
	got := ParsePytest(log)
	want := map[string]string{
		"tests/test_a.py::test_one":  StatusPassed,
		"tests/test_b.py::test_two":  StatusFailed,
		"tests/test_d.py::test_four": StatusSkipped,
		"tests/test_e.py::test_five": StatusError,
	}
	assertMap(t, got, want)
	// "SKIPPED [3] path:12: reason" is a summary line; [3] is a count, not a test.
	if _, ok := got["[3]"]; ok {
		t.Error("skip-summary count was recorded as a test")
	}
}

func TestParsePytestOptionsShortensPathParams(t *testing.T) {
	log := "PASSED tests/t.py::test_x[/very/long/path/to/case.txt]\n" +
		"PASSED tests/t.py::test_y[plain]\n"
	got := ParsePytestOptions(log)
	if _, ok := got["tests/t.py::test_x[/case.txt]"]; !ok {
		t.Errorf("path-like parameter not shortened; got keys %v", keys(got))
	}
	if got["tests/t.py::test_y[plain]"] != StatusPassed {
		t.Errorf("plain parameter should be untouched; got %v", keys(got))
	}
}

func TestParsePytestV2(t *testing.T) {
	// Colourised output, plus the older form where status trails the name.
	log := "\x1b[32mPASSED\x1b[0m tests/t.py::test_a\n" +
		"FAILED tests/t.py::test_b - ValueError: boom\n" +
		"tests/t.py::test_c PASSED\n"
	got := ParsePytestV2(log)
	if got["tests/t.py::test_a"] != StatusPassed {
		t.Errorf("colour codes not stripped; keys %v", keys(got))
	}
	if got["tests/t.py::test_b"] != StatusFailed {
		t.Errorf("assertion message not trimmed from FAILED id; keys %v", keys(got))
	}
	if got["tests/t.py::test_c"] != StatusPassed {
		t.Errorf("trailing-status form not handled; keys %v", keys(got))
	}
}

func TestParseDjango(t *testing.T) {
	log := `
test_one (auth.tests.AuthTests) ... ok
test_two (auth.tests.AuthTests) ... FAIL
test_three (auth.tests.AuthTests) ... skipped 'no db'
test_four (auth.tests.AuthTests) ... ERROR
FAIL: test_five (auth.tests.AuthTests)
ERROR: test_six (auth.tests.AuthTests)
`
	got := ParseDjango(log)
	want := map[string]string{
		"test_one (auth.tests.AuthTests)":   StatusPassed,
		"test_two (auth.tests.AuthTests)":   StatusFailed,
		"test_three (auth.tests.AuthTests)": StatusSkipped,
		"test_four (auth.tests.AuthTests)":  StatusError,
		"test_five":                         StatusFailed,
		"test_six":                          StatusError,
	}
	assertMap(t, got, want)
}

// Django's runner sometimes prints a long line between "..." and "ok"; the
// result still belongs to the preceding test.
func TestParseDjangoInterruptedOk(t *testing.T) {
	log := "test_seven (auth.tests.AuthTests) ... System check identified no issues (0 silenced).\nok\n"
	if got := ParseDjango(log); got["test_seven (auth.tests.AuthTests)"] != StatusPassed {
		t.Errorf("interrupted ok not attributed to the test; got %v", got)
	}
}

func TestParseSympy(t *testing.T) {
	log := `
test_alpha ok
test_beta F
test_gamma E
`
	assertMap(t, ParseSympy(log), map[string]string{
		"test_alpha": StatusPassed,
		"test_beta":  StatusFailed,
		"test_gamma": StatusError,
	})
}

func TestParserForKnownAndUnknown(t *testing.T) {
	if ParserFor("parse_log_astropy") == nil {
		t.Error("parse_log_astropy should alias to a real parser")
	}
	// An unknown parser must yield nil so the caller can refuse the instance
	// instead of scoring it with the wrong parser.
	if ParserFor("parse_log_nonexistent") != nil {
		t.Error("unknown parser name should return nil")
	}
}

func assertMap(t *testing.T, got, want map[string]string) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("status[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
