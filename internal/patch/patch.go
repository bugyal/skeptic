// Package patch parses unified diffs and can rebuild them with a hunk removed.
//
// Removing a hunk is how the partial control probes for weak tests: apply the
// reference fix minus one piece and see whether the suite notices.
package patch

import (
	"fmt"
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
