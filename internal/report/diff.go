package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/bugyal/skeptic/internal/check"
)

// Diff compares two runs task by task. It answers the question CI usually
// asks of a benchmark that already passed once: what changed since?
//
// A regression means a task gained a failure mode it did not have before:
// its nop started passing, its oracle started failing, or it started to flake.
// Nothing else is a regression. In particular a task that moved to or from
// ERROR or UNSUPPORTED is listed apart, because an ERROR is almost always the
// machine -- a full disk, a rate limit, a lost network -- and comparing it
// with a real verdict would report the host's bad day as the benchmark's.
type Diff struct {
	Old DiffSide `json:"old"`
	New DiffSide `json:"new"`

	Regressed    []Change `json:"regressed"`
	Fixed        []Change `json:"fixed"`
	Changed      []Change `json:"changed"`
	Uncomparable []Change `json:"uncomparable"`
	Added        []Change `json:"added"`
	Removed      []Change `json:"removed"`
	Unchanged    int      `json:"unchanged"`
}

// DiffSide identifies one of the two runs.
type DiffSide struct {
	Path           string `json:"path"`
	SkepticVersion string `json:"skeptic_version,omitempty"`
	GeneratedAt    string `json:"generated_at,omitempty"`
	Host           string `json:"host,omitempty"`
	Tasks          int    `json:"tasks"`
}

// Change is one task's move between runs. For an added task only New is set;
// for a removed one only Old.
type Change struct {
	ID        string `json:"id"`
	Old       string `json:"old,omitempty"`
	New       string `json:"new,omitempty"`
	OldReason string `json:"old_reason,omitempty"`
	NewReason string `json:"new_reason,omitempty"`
}

// HasRegressions reports whether any task gained a failure mode.
func (d Diff) HasRegressions() bool { return len(d.Regressed) > 0 }

// failureModes names what a verdict says is wrong with a task. CLEAN and
// NO_ORACLE say nothing is; ERROR and UNSUPPORTED say nothing could be
// checked, and never reach here.
func failureModes(v check.Verdict) map[string]bool {
	switch v {
	case check.VerdictNopPasses:
		return map[string]bool{"nop": true}
	case check.VerdictOracleFails:
		return map[string]bool{"oracle": true}
	case check.VerdictBoth:
		return map[string]bool{"nop": true, "oracle": true}
	case check.VerdictFlaky:
		return map[string]bool{"flaky": true}
	}
	return map[string]bool{}
}

func checkable(v check.Verdict) bool {
	return v != check.VerdictError && v != check.VerdictUnsupported
}

// Compare diffs two loaded reports. oldPath and newPath are recorded so the
// output says what was compared.
func Compare(old, cur Report, oldPath, newPath string) Diff {
	d := Diff{Old: side(old, oldPath), New: side(cur, newPath)}

	before := map[string]TaskReport{}
	for _, t := range old.Tasks {
		before[t.ID] = t
	}
	seen := map[string]bool{}
	for _, n := range cur.Tasks {
		seen[n.ID] = true
		o, ok := before[n.ID]
		if !ok {
			d.Added = append(d.Added, Change{ID: n.ID, New: n.Verdict, NewReason: n.Reason})
			continue
		}
		c := Change{ID: n.ID, Old: o.Verdict, New: n.Verdict, OldReason: o.Reason, NewReason: n.Reason}
		ov, nv := check.Verdict(o.Verdict), check.Verdict(n.Verdict)
		switch {
		case ov == nv:
			d.Unchanged++
		case !checkable(ov) || !checkable(nv):
			d.Uncomparable = append(d.Uncomparable, c)
		default:
			was, is := failureModes(ov), failureModes(nv)
			gained, lost := false, false
			for m := range is {
				gained = gained || !was[m]
			}
			for m := range was {
				lost = lost || !is[m]
			}
			switch {
			case gained:
				d.Regressed = append(d.Regressed, c)
			case lost:
				d.Fixed = append(d.Fixed, c)
			default:
				// CLEAN <-> NO_ORACLE: no failure mode either side, but the
				// oracle's availability changed, which a reader should see.
				d.Changed = append(d.Changed, c)
			}
		}
	}
	for _, o := range old.Tasks {
		if !seen[o.ID] {
			d.Removed = append(d.Removed, Change{ID: o.ID, Old: o.Verdict, OldReason: o.Reason})
		}
	}
	for _, cs := range [][]Change{d.Regressed, d.Fixed, d.Changed, d.Uncomparable, d.Added, d.Removed} {
		sort.Slice(cs, func(i, j int) bool { return cs[i].ID < cs[j].ID })
	}
	return d
}

func side(r Report, path string) DiffSide {
	s := DiffSide{Path: path, SkepticVersion: r.SkepticVersion, Tasks: len(r.Tasks)}
	if !r.GeneratedAt.IsZero() {
		s.GeneratedAt = r.GeneratedAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	if r.Host.OS != "" {
		s.Host = r.Host.OS + "/" + r.Host.Arch
	}
	return s
}

// diffSection is one titled group in the rendered output. The order here is
// the order a reader should act in.
type diffSection struct {
	title, note string
	changes     []Change
}

func (d Diff) sections() []diffSection {
	return []diffSection{
		{"Regressed", "gained a failure mode", d.Regressed},
		{"Fixed", "lost a failure mode", d.Fixed},
		{"Changed", "different verdict, no failure mode gained or lost", d.Changed},
		{"Could not compare", "one side is ERROR or UNSUPPORTED; usually the machine, not the benchmark", d.Uncomparable},
		{"Added", "only in the new run", d.Added},
		{"Removed", "only in the old run", d.Removed},
	}
}

// Summary is the one-line count of every section.
func (d Diff) Summary() string {
	return fmt.Sprintf("%d regressed · %d fixed · %d changed · %d could not compare · %d added · %d removed · %d unchanged",
		len(d.Regressed), len(d.Fixed), len(d.Changed), len(d.Uncomparable), len(d.Added), len(d.Removed), d.Unchanged)
}

// Table writes the diff for a terminal.
func (d Diff) Table(w io.Writer) {
	fmt.Fprintf(w, "old  %s  (%s)\nnew  %s  (%s)\n", d.Old.Path, describe(d.Old), d.New.Path, describe(d.New))
	for _, s := range d.sections() {
		if len(s.changes) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s — %s\n", s.title, s.note)
		width := 0
		for _, c := range s.changes {
			width = max(width, len(c.ID))
		}
		for _, c := range s.changes {
			fmt.Fprintf(w, "  %-*s  %s\n", width, c.ID, transition(c))
		}
	}
	fmt.Fprintf(w, "\n%s\n", d.Summary())
}

// Markdown writes the diff for an issue or a pull request comment.
func (d Diff) Markdown(w io.Writer) {
	fmt.Fprintf(w, "## Skeptic diff\n\n| | Run | Skeptic | When | Host | Tasks |\n| --- | --- | --- | --- | --- | ---: |\n")
	for _, s := range []struct {
		label string
		side  DiffSide
	}{{"old", d.Old}, {"new", d.New}} {
		fmt.Fprintf(w, "| %s | `%s` | %s | %s | %s | %d |\n",
			s.label, s.side.Path, s.side.SkepticVersion, s.side.GeneratedAt, s.side.Host, s.side.Tasks)
	}
	fmt.Fprintf(w, "\n%s\n", d.Summary())
	for _, s := range d.sections() {
		if len(s.changes) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n### %s\n\n_%s_\n\n| Task | Old | New | New reason |\n| --- | --- | --- | --- |\n", s.title, s.note)
		for _, c := range s.changes {
			fmt.Fprintf(w, "| `%s` | %s | %s | %s |\n", c.ID, orDash(c.Old), orDash(c.New), c.NewReason)
		}
	}
}

// JSON writes the diff as indented JSON.
func (d Diff) JSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(d)
}

func describe(s DiffSide) string {
	out := fmt.Sprintf("%d tasks", s.Tasks)
	for _, p := range []string{s.SkepticVersion, s.GeneratedAt, s.Host} {
		if p != "" {
			out += ", " + p
		}
	}
	return out
}

func transition(c Change) string {
	switch {
	case c.Old == "":
		return fmt.Sprintf("%s  %s", c.New, c.NewReason)
	case c.New == "":
		return fmt.Sprintf("%s  (was)", c.Old)
	}
	return fmt.Sprintf("%s → %s  %s", c.Old, c.New, c.NewReason)
}

func orDash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}
