package swebench

import "sort"

// EvalType values from the dataset.
const (
	EvalPassAndFail = "pass_and_fail"
	EvalFailOnly    = "fail_only"
)

// resolveCase finds the status-map key for an expected test id.
//
// A port of grading.py's _resolve_case: 676 expected ids in SWE-bench Verified
// are truncated mid-parameter, so for those only (unbalanced "["), a prefix
// match is allowed when every candidate agrees on pass-versus-fail. Exact ids
// keep exact-match semantics.
//
// Candidates are sorted before picking one. Go map iteration is randomised, and
// an unstable pick would make verdicts irreproducible; the agreement guard means
// the choice cannot change the outcome, only the determinism of getting there.
func resolveCase(name string, sm map[string]string) (string, bool) {
	if _, ok := sm[name]; ok {
		return name, true
	}
	if countRune(name, '[') <= countRune(name, ']') {
		return "", false
	}

	var matches []string
	for k := range sm {
		if len(k) >= len(name) && k[:len(name)] == name {
			matches = append(matches, k)
		}
	}
	if len(matches) == 0 {
		return "", false
	}
	sort.Strings(matches)

	first := isPassing(sm[matches[0]])
	for _, k := range matches[1:] {
		if isPassing(sm[k]) != first {
			return "", false // candidates disagree; refuse to guess
		}
	}
	return matches[0], true
}

func isPassing(status string) bool { return status == StatusPassed || status == StatusXfail }

// testPassed is the FAIL_TO_PASS rule: the test must now pass outright.
func testPassed(name string, sm map[string]string) bool {
	k, ok := resolveCase(name, sm)
	return ok && isPassing(sm[k])
}

// testMaintained is the PASS_TO_PASS rule. A skipped test is not a regression
// here, unlike for FAIL_TO_PASS, where skipping is how a patch can dodge the
// test it was supposed to fix.
func testMaintained(name string, sm map[string]string) bool {
	k, ok := resolveCase(name, sm)
	return testPassed(name, sm) || (ok && sm[k] == StatusSkipped)
}

// Outcome records why an instance did or did not resolve.
type Outcome struct {
	Resolved     bool
	FailToPass   int
	FailToPassOK int
	PassToPass   int
	PassToPassOK int
	// Missing lists expected tests absent from the output entirely, the usual
	// sign that the suite never ran rather than that it ran and failed.
	Missing []string
}

// Score is 1.0 only when every FAIL_TO_PASS test passes and every PASS_TO_PASS
// test still holds, as the brief requires. Partial credit is not available:
// upstream treats resolution as binary and inventing a fraction here would make
// the number mean something SWE-bench never intended.
func Score(sm map[string]string, failToPass, passToPass []string, evalType string) (float64, Outcome) {
	o := Outcome{FailToPass: len(failToPass), PassToPass: len(passToPass)}

	for _, name := range failToPass {
		if testPassed(name, sm) {
			o.FailToPassOK++
		} else if _, ok := resolveCase(name, sm); !ok {
			o.Missing = append(o.Missing, name)
		}
	}

	// fail_only instances carry no meaningful PASS_TO_PASS set.
	if evalType != EvalFailOnly {
		for _, name := range passToPass {
			if testMaintained(name, sm) {
				o.PassToPassOK++
			} else if _, ok := resolveCase(name, sm); !ok {
				o.Missing = append(o.Missing, name)
			}
		}
	} else {
		o.PassToPass = 0
	}

	o.Resolved = o.FailToPassOK == o.FailToPass && o.PassToPassOK == o.PassToPass
	if o.Resolved {
		return 1, o
	}
	return 0, o
}

func countRune(s string, r rune) int {
	n := 0
	for _, c := range s {
		if c == r {
			n++
		}
	}
	return n
}
