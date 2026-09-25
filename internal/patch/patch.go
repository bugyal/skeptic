// Package patch parses unified diffs and can rebuild them with a hunk removed.
//
// Removing a hunk is how the partial control probes for weak tests: apply the
// reference fix minus one piece and see whether the suite notices.
package patch

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// hunkHeader matches "@@ -12,7 +12,9 @@ optional section heading".
var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$`)

// Hunk is one contiguous change within a file.
type Hunk struct {
	OldStart, OldCount int
	NewStart, NewCount int
	Section            string   // trailing text on the @@ line
	Lines              []string // body: context, -removed and +added lines
}

// Delta is how many lines this hunk adds to the file, negative if it removes.
func (h Hunk) Delta() int { return h.NewCount - h.OldCount }

// File is one file's worth of a diff.
type File struct {
	Header []string // everything before the first @@ line
	Path   string   // the new-side path, for reporting
	Hunks  []Hunk
}

// Patch is a parsed unified diff.
type Patch struct{ Files []File }

// Parse reads a unified diff. Content before the first file header (commit
// messages, git's "---" separator) is discarded, since only the diff is applied.
func Parse(s string) (*Patch, error) {
	p := &Patch{}
	// Split after dropping one trailing newline: otherwise the final empty
	// element is consumed as a context line and round-tripping gains a blank.
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")

	var cur *File
	for i := 0; i < len(lines); i++ {
		line := lines[i]

		switch {
		case strings.HasPrefix(line, "diff --git "):
			p.flush(&cur)
			cur = &File{Header: []string{line}}

		case strings.HasPrefix(line, "--- ") && cur == nil:
			// A bare diff with no "diff --git" preamble.
			cur = &File{Header: []string{line}}

		case hunkHeader.MatchString(line):
			if cur == nil {
				return nil, fmt.Errorf("hunk at line %d has no file header", i+1)
			}
			h, err := parseHunk(line)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", i+1, err)
			}
			// Consume the body: everything until the next hunk or file header.
			for i+1 < len(lines) {
				next := lines[i+1]
				if hunkHeader.MatchString(next) || strings.HasPrefix(next, "diff --git ") {
					break
				}
				// A body line starts with space, +, - or \ (no newline marker).
				// An empty line is a context line whose trailing space was
				// stripped, which is common and must not end the hunk.
				if next != "" && !strings.ContainsAny(next[:1], " +-\\") {
					break
				}
				h.Lines = append(h.Lines, next)
				i++
			}
			cur.Hunks = append(cur.Hunks, h)

		case cur != nil && len(cur.Hunks) == 0:
			cur.Header = append(cur.Header, line)
			if strings.HasPrefix(line, "+++ ") {
				cur.Path = strings.TrimPrefix(strings.Fields(line)[1], "b/")
			}
		}
	}
	p.flush(&cur)

	if len(p.Files) == 0 {
		return nil, fmt.Errorf("no file diffs found")
	}
	return p, nil
}

func (p *Patch) flush(cur **File) {
	if *cur != nil && len((*cur).Hunks) > 0 {
		p.Files = append(p.Files, **cur)
	}
	*cur = nil
}

func parseHunk(line string) (Hunk, error) {
	m := hunkHeader.FindStringSubmatch(line)
	if m == nil {
		return Hunk{}, fmt.Errorf("malformed hunk header %q", line)
	}
	atoi := func(s string) int {
		if s == "" {
			return 1 // an omitted count means exactly one line
		}
		n, _ := strconv.Atoi(s)
		return n
	}
	return Hunk{
		OldStart: atoi(m[1]), OldCount: atoi(m[2]),
		NewStart: atoi(m[3]), NewCount: atoi(m[4]),
		Section: m[5],
	}, nil
}

// HunkCount is the total number of hunks across every file.
func (p *Patch) HunkCount() int {
	n := 0
	for _, f := range p.Files {
		n += len(f.Hunks)
	}
	return n
}

// Locate maps a flat hunk index onto its file and position within that file.
func (p *Patch) Locate(index int) (fileIdx, hunkIdx int, ok bool) {
	for fi, f := range p.Files {
		if index < len(f.Hunks) {
			return fi, index, true
		}
		index -= len(f.Hunks)
	}
	return 0, 0, false
}

// Without returns a copy of the patch with one hunk removed, identified by its
// flat index. Later hunks in the same file are renumbered, because dropping a
// hunk shifts every following new-side line number by its delta and git apply
// rejects a diff whose offsets do not add up.
//
// A file left with no hunks is dropped entirely.
func (p *Patch) Without(index int) (*Patch, Hunk, error) {
	fi, hi, ok := p.Locate(index)
	if !ok {
		return nil, Hunk{}, fmt.Errorf("hunk index %d out of range (patch has %d)", index, p.HunkCount())
	}

	out := &Patch{}
	for i, f := range p.Files {
		if i != fi {
			out.Files = append(out.Files, f)
			continue
		}
		nf := File{Header: f.Header, Path: f.Path}
		delta := f.Hunks[hi].Delta()
		for j, h := range f.Hunks {
			if j == hi {
				continue
			}
			if j > hi {
				h.NewStart -= delta
			}
			nf.Hunks = append(nf.Hunks, h)
		}
		if len(nf.Hunks) > 0 {
			out.Files = append(out.Files, nf)
		}
	}
	return out, p.Files[fi].Hunks[hi], nil
}

// String renders the patch back to unified diff text.
func (p *Patch) String() string {
	var b strings.Builder
	for _, f := range p.Files {
		for _, h := range f.Header {
			b.WriteString(h)
			b.WriteByte('\n')
		}
		for _, h := range f.Hunks {
			b.WriteString(h.String())
		}
	}
	return b.String()
}

// String renders one hunk, header included.
func (h Hunk) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "@@ -%s +%s @@%s\n", rangeOf(h.OldStart, h.OldCount), rangeOf(h.NewStart, h.NewCount), h.Section)
	for _, l := range h.Lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}

// rangeOf formats a hunk range, omitting the count when it is 1 as git does.
func rangeOf(start, count int) string {
	if count == 1 {
		return strconv.Itoa(start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}

// Describe names a hunk for a report: the file and the lines it touches.
func (p *Patch) Describe(index int) string {
	fi, hi, ok := p.Locate(index)
	if !ok {
		return "unknown hunk"
	}
	f := p.Files[fi]
	h := f.Hunks[hi]
	return fmt.Sprintf("%s lines %d-%d", f.Path, h.NewStart, h.NewStart+h.NewCount-1)
}

// commentPrefixes covers the line-comment syntax of the languages benchmark
// repositories actually use.
var commentPrefixes = []string{"#", "//", "--", ";", "%", "*", "/*", "*/"}

// Semantic reports whether a hunk changes anything a test could observe.
//
// A hunk that only edits comments or blank lines cannot be graded by any test,
// so withholding it proves nothing about the test suite. Treating such a hunk
// as evidence of weak tests is a false positive: the first real instance the
// partial control flagged, astropy__astropy-13398, turned out to include a
// hunk whose entire content was fixing "siderial" to "sidereal" in a comment.
func (h Hunk) Semantic() bool {
	for _, l := range h.Lines {
		if len(l) == 0 {
			continue
		}
		switch l[0] {
		case '+', '-':
		default:
			continue // context line
		}
		body := strings.TrimSpace(l[1:])
		if body == "" {
			continue // blank line added or removed
		}
		if !isComment(body) {
			return true
		}
	}
	return false
}

func isComment(s string) bool {
	for _, p := range commentPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// SemanticHunks counts the hunks that change observable behaviour.
func (p *Patch) SemanticHunks() int {
	n := 0
	for _, f := range p.Files {
		for _, h := range f.Hunks {
			if h.Semantic() {
				n++
			}
		}
	}
	return n
}

// DeletionOnly reports whether a hunk only removes lines.
//
// Withholding such a hunk leaves the removed code in place. When the change is
// a cleanup -- deleting a method that nothing calls any more after another
// hunk landed -- that is unobservable by construction, and the tests passing
// says nothing about their quality.
//
// django__django-13121 is the case in point: a refactor removing
// date_interval_sql() from three database backends. Every deletion hunk could
// be withheld with the suite still at full marks, because dead code is dead.
// Reporting that as a weak test would be wrong.
//
// It is a heuristic, not a proof: deleting code that IS still called would
// break things, and the tests failing to notice would be a real finding. So
// this classifies rather than filters, and the caller reports the distinction.
func (h Hunk) DeletionOnly() bool {
	var removed, added int
	for _, l := range h.Lines {
		if len(l) == 0 {
			continue
		}
		body := strings.TrimSpace(l[1:])
		switch l[0] {
		case '-':
			if body != "" {
				removed++
			}
		case '+':
			if body != "" {
				added++
			}
		}
	}
	return removed > 0 && added == 0
}

// importLine matches the import syntax of the languages benchmark repositories
// use. Deliberately conservative: it must match the whole changed line.
var importLine = regexp.MustCompile(
	`^(?:from\s+[\w./]+\s+import\s+.*|import\s+.*|` + // python, js, go, java
		`const\s+\w+\s*=\s*require\(.*\)|use\s+[\w:]+.*;)$`) // node, rust

// importContinuation matches a line inside a parenthesised import list:
// identifiers, commas, whitespace and an optional closing paren, nothing else.
// A line with a call, operator or assignment is not one.
var importContinuation = regexp.MustCompile(`^[\w\s,]+\)?,?$`)

// hasImportContext reports whether any line of the hunk, changed or context,
// opens an import. Used to decide whether a bare identifier list is a
// continuation of one rather than, say, a tuple literal.
func (h Hunk) hasImportContext() bool {
	for _, l := range h.Lines {
		body := l
		if len(l) > 0 && (l[0] == '+' || l[0] == '-' || l[0] == ' ') {
			body = l[1:]
		}
		if importLine.MatchString(strings.TrimSpace(body)) {
			return true
		}
	}
	return false
}

// ImportOnly reports whether every changed line in a hunk is an import.
//
// Gold patches routinely bundle the fix with the cleanup the fix enables, and
// removing a now-unused import is the commonest form. django__django-15368 is
// the example: the real hunk replaced isinstance(attr, Expression) with
// hasattr(attr, 'resolve_expression'), and a second hunk dropped Expression
// from the import list. Withholding that second hunk leaves an unused import,
// which no test can observe.
//
// DeletionOnly does not catch it, because rewriting an import line both adds
// and removes.
func (h Hunk) ImportOnly() bool {
	changed := 0
	for _, l := range h.Lines {
		if len(l) == 0 || (l[0] != '+' && l[0] != '-') {
			continue
		}
		body := strings.TrimSpace(l[1:])
		if body == "" || isComment(body) {
			continue
		}
		// A parenthesised import spans several lines, and the changed line is
		// often a continuation rather than the statement itself:
		//
		//   from sympy import (log, sqrt, pi,
		//  -                   Lambda, erf, I)
		//  +                   Lambda, erf, I, uppergamma, hyper)
		//
		// sympy__sympy-13878 turned on exactly that shape.
		if !importLine.MatchString(body) &&
			!(h.hasImportContext() && importContinuation.MatchString(body)) {
			return false
		}
		changed++
	}
	return changed > 0
}

// Unobservable reports whether withholding a hunk could not, by construction,
// be detected by any test: comments and blank lines, pure deletions of code
// the rest of the patch stops calling, and imports left unused by the fix.
//
// It is a heuristic over a real pattern -- gold patches bundle the fix with
// the cleanup the fix enables -- not a proof. It informs how a result is
// reported; it never suppresses one.
func (h Hunk) Unobservable() (bool, string) {
	switch {
	case !h.Semantic():
		return true, "comments or blank lines only"
	case h.ImportOnly():
		return true, "import statements only"
	case h.DocstringOnly():
		return true, "documentation prose only"
	case h.DeletionOnly():
		return true, "deletion only; likely cleanup"
	}
	return false, ""
}

// SampleHunks picks up to n hunk indices spread across the patch rather than
// taking the first n.
//
// Taking the first n biases the sample to whichever file sorts first.
// django__django-16560 is the case: 18 hunks across two files, and the first
// three all landed in django/contrib/postgres/constraints.py — code the
// SQLite-backed test environment never executes. Three "ungraded" hunks that
// said nothing about the suite, because the sample never reached the file the
// tests actually exercise.
//
// Files are visited round-robin so every file is represented before any file
// is sampled twice, and within a file the hunks are spread evenly. The result
// is deterministic, so reruns agree.
func (p *Patch) SampleHunks(n int) []int {
	if n <= 0 {
		return nil
	}

	// Flat index of the first hunk of each file.
	offsets := make([]int, len(p.Files))
	at := 0
	for i, f := range p.Files {
		offsets[i] = at
		at += len(f.Hunks)
	}

	var out []int
	taken := make([]int, len(p.Files)) // hunks taken per file so far
	for len(out) < n {
		progressed := false
		for fi, f := range p.Files {
			if len(out) >= n {
				break
			}
			if taken[fi] >= len(f.Hunks) {
				continue
			}
			// Spread within the file: the k-th pick of m total lands at
			// k*len/m, so picks are distributed rather than clustered.
			quota := n/len(p.Files) + 1
			if quota > len(f.Hunks) {
				quota = len(f.Hunks)
			}
			idx := taken[fi] * len(f.Hunks) / quota
			if idx >= len(f.Hunks) {
				idx = len(f.Hunks) - 1
			}
			out = append(out, offsets[fi]+idx)
			taken[fi]++
			progressed = true
		}
		if !progressed {
			break // every hunk exhausted
		}
	}

	// Deduplicate while preserving order; uneven spreads can collide.
	seen := map[int]bool{}
	var uniq []int
	for _, i := range out {
		if !seen[i] {
			seen[i] = true
			uniq = append(uniq, i)
		}
	}
	return uniq
}

// nonExecutablePrefixes are directories whose contents a project's unit tests
// do not import: gallery examples, documentation, benchmarks, tooling.
var nonExecutablePrefixes = []string{
	"examples/", "example/", "doc/", "docs/", "benchmarks/", "benchmark/",
	"tools/", "scripts/", "changelog/", "changes/",
}

// NonExecutable reports whether a file lives somewhere the test suite does not
// import from, so a change to it cannot be graded by unit tests.
//
// scikit-learn__scikit-learn-12682 flagged a hunk in
// examples/decomposition/plot_sparse_coding.py. Gallery scripts are rendered
// by the documentation build, not imported by tests, so withholding a change
// to one proves nothing about the suite.
func (f File) NonExecutable() bool {
	p := strings.TrimPrefix(filepath.ToSlash(f.Path), "./")
	for _, pre := range nonExecutablePrefixes {
		if strings.HasPrefix(p, pre) || strings.Contains(p, "/"+pre) {
			return true
		}
	}
	return false
}

var (
	// rstDirective matches ".. versionadded:: 0.21" and friends.
	rstDirective = regexp.MustCompile(`^\.\.\s+[\w-]+::`)
	// numpydocField matches "warm_start : bool, optional (default=False)".
	numpydocField = regexp.MustCompile(`^[\w*]+\s+:\s+\S`)
	// sectionUnderline matches the "----------" under a numpydoc heading.
	sectionUnderline = regexp.MustCompile(`^[-=~^"]{3,}$`)
	// bareCall matches a statement that is only a call: renderer.close_group(x).
	// Guarded against explicitly, because a hunk adding one is real code and
	// matplotlib__matplotlib-24637 -- a confirmed finding -- is exactly that
	// shape.
	bareCall = regexp.MustCompile(`^[\w.\[\]]+\(.*\)\s*$`)
	// statementStart matches lines opening a Python statement.
	statementStart = regexp.MustCompile(`^(def |class |return\b|if |elif |else\b|for |while |import |from |try\b|except|finally|with |raise |assert |yield|pass\b|break\b|continue\b|@\w)`)
)

// looksLikeCode reports whether a line would plausibly execute.
func looksLikeCode(s string) bool {
	switch {
	case statementStart.MatchString(s), bareCall.MatchString(s):
		return true
	case strings.Contains(s, "=") && !strings.Contains(s, "=="):
		// An assignment, but not a numpydoc default like "(default=False)".
		return !numpydocField.MatchString(s)
	}
	return false
}

// DocstringOnly reports whether a hunk changes only documentation prose.
//
// Deliberately narrow: it requires a documentation construct to be present --
// an RST directive, a numpydoc field, a section underline -- and refuses if any
// changed line looks like code. A loose "is this prose" test would classify a
// hunk of bare calls as documentation, and matplotlib__matplotlib-24637, a
// confirmed weak test, is precisely a pair of bare calls.
//
// Three instances in the sampled batch turned on this shape:
// sympy__sympy-13852 (a doctest's expected output),
// scikit-learn__scikit-learn-12682 and -13496 (parameter documentation).
func (h Hunk) DocstringOnly() bool {
	hasDocConstruct, changed := false, 0
	for _, l := range h.Lines {
		if len(l) == 0 || (l[0] != '+' && l[0] != '-') {
			continue
		}
		body := strings.TrimSpace(l[1:])
		if body == "" {
			continue
		}
		changed++
		if looksLikeCode(body) {
			return false
		}
		if rstDirective.MatchString(body) || numpydocField.MatchString(body) ||
			sectionUnderline.MatchString(body) || strings.HasPrefix(body, ">>>") {
			hasDocConstruct = true
		}
	}
	return changed > 0 && hasDocConstruct
}
