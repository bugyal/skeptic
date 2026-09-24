// Package swebench adapts SWE-bench instances.
//
// The log parsers below are ports of swebench/harness/log_parsers/python.py.
// Scoring depends on recovering each test's status exactly as the upstream
// harness does, so these follow the originals line for line, quirks included.
package swebench

import (
	"regexp"
	"strings"
)

// Test statuses, matching swebench.harness.constants.TestStatus.
const (
	StatusFailed  = "FAILED"
	StatusPassed  = "PASSED"
	StatusSkipped = "SKIPPED"
	StatusError   = "ERROR"
	StatusXfail   = "XFAIL"
)

var allStatuses = []string{StatusFailed, StatusPassed, StatusSkipped, StatusError, StatusXfail}

var (
	skipSummaryCount = regexp.MustCompile(`^\[\d+\]$`)
	optionPattern    = regexp.MustCompile(`^(.*?)\[(.*)\]`)
	ansiPattern      = regexp.MustCompile(`\[(\d+)m`)
	sympyFailPattern = regexp.MustCompile(`(_*) (.*)\.py:(.*) (_*)`)
	djangoInterrupt  = []*regexp.Regexp{
		regexp.MustCompile(`(?m)^(.*?)\s\.\.\.\sTesting against Django installed in ((?s:.*?)) silenced\)\.\nok$`),
		regexp.MustCompile(`(?m)^(.*?)\s\.\.\.\sInternal Server Error: /(.*)/\nok$`),
		regexp.MustCompile(`(?m)^(.*?)\s\.\.\.\sSystem check identified no issues \(0 silenced\)\nok$`),
	}
)

func startsWithStatus(line string) bool {
	for _, s := range allStatuses {
		if strings.HasPrefix(line, s) {
			return true
		}
	}
	return false
}

func endsWithStatus(line string) bool {
	for _, s := range allStatuses {
		if strings.HasSuffix(line, s) {
			return true
		}
	}
	return false
}

// isSkipSummary matches pytest's "SKIPPED [3] path:12: reason" summary, whose
// second field is a count rather than a test name. Deliberately limited to
// SKIPPED: PASS_TO_PASS for two pytest instances literally expects "[100%]".
func isSkipSummary(status, name string) bool {
	return status == StatusSkipped && skipSummaryCount.MatchString(name)
}

// Parser turns raw test output into a test-name to status map.
type Parser func(log string) map[string]string

// parserByName maps the dataset's log_parser field to an implementation.
// Aliases mirror the assignments at the foot of python.py.
var parserByName = map[string]Parser{
	"parse_log_pytest":         ParsePytest,
	"parse_log_pytest_options": ParsePytestOptions,
	"parse_log_pytest_v2":      ParsePytestV2,
	"parse_log_django":         ParseDjango,
	"parse_log_sympy":          ParseSympy,
	"parse_log_seaborn":        ParseSeaborn,
	"parse_log_matplotlib":     ParseMatplotlib,

	"parse_log_astroid":     ParsePytest,
	"parse_log_flask":       ParsePytest,
	"parse_log_marshmallow": ParsePytest,
	"parse_log_pvlib":       ParsePytest,
	"parse_log_pyvista":     ParsePytest,
	"parse_log_sqlfluff":    ParsePytest,
	"parse_log_xarray":      ParsePytest,

	"parse_log_pydicom":  ParsePytestOptions,
	"parse_log_requests": ParsePytestOptions,
	"parse_log_pylint":   ParsePytestOptions,

	"parse_log_astropy": ParsePytestV2,
	"parse_log_scikit":  ParsePytestV2,
	"parse_log_sphinx":  ParsePytestV2,
}

// ParserFor returns the parser named by a dataset row. An unknown name yields
// nil, and the caller reports the instance as unsupported rather than scoring
// it with a parser that was never written for it.
func ParserFor(name string) Parser { return parserByName[name] }

// ParsePytest handles standard pytest output.
func ParsePytest(log string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(log, "\n") {
		if !startsWithStatus(line) {
			continue
		}
		if strings.HasPrefix(line, StatusFailed) {
			line = strings.ReplaceAll(line, " - ", " ")
		}
		f := strings.Fields(line)
		if len(f) <= 1 || isSkipSummary(f[0], f[1]) {
			continue
		}
		out[f[1]] = f[0]
	}
	return out
}

// ParseMatplotlib is pytest output with mouse-button names normalised, as
// matplotlib's own test ids record them numerically.
func ParseMatplotlib(log string) map[string]string {
	r := strings.NewReplacer("MouseButton.LEFT", "1", "MouseButton.RIGHT", "3")
	return ParsePytest(r.Replace(log))
}

// ParsePytestOptions handles pytest ids carrying a bracketed parameter, where
// a path-like parameter is shortened to its last segment.
func ParsePytestOptions(log string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(log, "\n") {
		if !startsWithStatus(line) {
			continue
		}
		if strings.HasPrefix(line, StatusFailed) {
			line = strings.ReplaceAll(line, " - ", " ")
		}
		f := strings.Fields(line)
		if len(f) <= 1 || isSkipSummary(f[0], f[1]) {
			continue
		}
		name := f[1]
		if m := optionPattern.FindStringSubmatch(name); m != nil {
			main, option := m[1], m[2]
			if strings.HasPrefix(option, "/") && !strings.HasPrefix(option, "//") && !strings.Contains(option, "*") {
				parts := strings.Split(option, "/")
				option = "/" + parts[len(parts)-1]
			}
			name = main + "[" + option + "]"
		}
		out[name] = f[0]
	}
	return out
}

// ParsePytestV2 handles later pytest versions, stripping colour codes and
// tolerating output where the status trails the test name.
func ParsePytestV2(log string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(log, "\n") {
		line = ansiPattern.ReplaceAllString(line, "")
		line = stripControl(line)

		if startsWithStatus(line) {
			if strings.HasPrefix(line, StatusFailed) {
				line, _, _ = strings.Cut(line, " - ")
			}
			f := strings.Fields(line)
			if len(f) >= 2 && !isSkipSummary(f[0], f[1]) {
				out[strings.Join(f[1:], " ")] = f[0]
			}
			continue
		}
		if endsWithStatus(line) {
			f := strings.Fields(line)
			if len(f) >= 2 {
				out[strings.Join(f[:len(f)-1], " ")] = f[len(f)-1]
			}
		}
	}
	return out
}

// stripControl removes C0 control characters, which colourised pytest output
// interleaves with test names.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 1 && r < 32 {
			return -1
		}
		return r
	}, s)
}

// ParseSympy handles sympy's bespoke runner.
func ParseSympy(log string) map[string]string {
	out := map[string]string{}
	for _, m := range sympyFailPattern.FindAllStringSubmatch(log, -1) {
		out[m[2]+".py:"+m[3]] = StatusFailed
	}
	for _, line := range strings.Split(log, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "test_") {
			continue
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch {
		case strings.HasSuffix(line, " E"):
			out[f[0]] = StatusError
		case strings.HasSuffix(line, " F"):
			out[f[0]] = StatusFailed
		case strings.HasSuffix(line, " ok"):
			out[f[0]] = StatusPassed
		}
	}
	return out
}

// ParseSeaborn handles seaborn's output, where a pass can appear mid-line.
func ParseSeaborn(log string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(log, "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		switch {
		case strings.HasPrefix(line, StatusFailed):
			out[parts[1]] = StatusFailed
		case strings.Contains(line, " "+StatusPassed+" "):
			if parts[1] == StatusPassed {
				out[parts[0]] = StatusPassed
			}
		case strings.HasPrefix(line, StatusPassed):
			out[parts[1]] = StatusPassed
		}
	}
	return out
}

// ParseDjango handles Django's unittest runner, including the known cases
// where a long log line interrupts a result and its "ok" lands on its own line.
func ParseDjango(log string) map[string]string {
	out := map[string]string{}
	prevTest := ""

	for _, raw := range strings.Split(log, "\n") {
		line := strings.TrimSpace(raw)

		if strings.Contains(line, "--version is equivalent to version") {
			out["--version is equivalent to version"] = StatusPassed
		}
		if strings.Contains(line, " ... ") {
			prevTest = strings.SplitN(line, " ... ", 2)[0]
		}

		matched := false
		for _, suffix := range []string{" ... ok", " ... OK", " ...  OK"} {
			if strings.HasSuffix(line, suffix) {
				l := line
				// A migration-progress line can be concatenated onto the test
				// name; keep only the part after the ellipsis.
				if strings.HasPrefix(l, "Applying sites.0002_alter_domain_unique...test_no_migrations") {
					parts := strings.SplitN(l, "...", 2)
					l = strings.TrimSpace(parts[len(parts)-1])
				}
				idx := strings.LastIndex(l, suffix)
				out[l[:idx]] = StatusPassed
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		switch {
		case strings.Contains(line, " ... skipped"):
			out[strings.SplitN(line, " ... skipped", 2)[0]] = StatusSkipped
		case strings.HasSuffix(line, " ... FAIL"):
			out[strings.SplitN(line, " ... FAIL", 2)[0]] = StatusFailed
		case strings.HasSuffix(line, " ... ERROR"):
			out[strings.SplitN(line, " ... ERROR", 2)[0]] = StatusError
		}
		if strings.HasPrefix(line, "FAIL:") {
			if f := strings.Fields(line); len(f) > 1 {
				out[f[1]] = StatusFailed
			}
		}
		if strings.HasPrefix(line, "ERROR:") {
			if f := strings.Fields(line); len(f) > 1 {
				out[f[1]] = StatusError
			}
		}
		if strings.HasPrefix(line, "ok") && prevTest != "" {
			out[prevTest] = StatusPassed
		}
	}

	for _, re := range djangoInterrupt {
		for _, m := range re.FindAllStringSubmatch(log, -1) {
			out[m[1]] = StatusPassed
		}
	}
	return out
}
