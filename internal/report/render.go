package report

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/skeptic-labs/skeptic/internal/check"
)

// ANSI colours, emitted only to a terminal. No rendering library is used:
// the table is four columns wide and a dependency would cost more than it saves.
const (
	ansiReset  = "\033[0m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiGrey   = "\033[90m"
	ansiBold   = "\033[1m"
)

// IsTerminal reports whether w is an interactive terminal, so colour can be
// dropped when output is piped or captured by CI.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

type painter struct{ on bool }

func (p painter) paint(code, s string) string {
	if !p.on {
		return s
	}
	return code + s + ansiReset
}

func (p painter) verdict(v string) string {
	switch check.Verdict(v) {
	case check.VerdictClean:
		return p.paint(ansiGreen, v)
	case check.VerdictNoOracle, check.VerdictUnsupported:
		return p.paint(ansiGrey, v)
	case check.VerdictError:
		return p.paint(ansiYellow, v)
	default:
		return p.paint(ansiRed, v)
	}
}

// Table renders the report as a plain-text table, flagged tasks first.
func (r Report) Table(w io.Writer) {
	p := painter{on: IsTerminal(w)}

	idWidth := len("TASK")
	for _, t := range r.Tasks {
		if len(t.ID) > idWidth {
			idWidth = len(t.ID)
		}
	}
	if idWidth > 48 {
		idWidth = 48
	}

	fmt.Fprintf(w, "%-14s  %-*s  %5s  %6s  %s\n",
		"VERDICT", idWidth, "TASK", "NOP", "ORACLE", "REASON")

	for _, t := range r.Tasks {
		id := t.ID
		if len(id) > idWidth {
			id = id[:idWidth-1] + "…"
		}
		// The verdict is padded before colouring, so escape codes never
		// disturb column alignment.
		fmt.Fprintf(w, "%s  %-*s  %5s  %6s  %s\n",
			p.verdict(pad(t.Verdict, 14)), idWidth, id,
			score(t.NopScore), score(t.OracleScore),
			p.paint(ansiGrey, t.Reason))
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, r.Summary(p.on))
}

// Summary is the one-line tally printed under the table.
func (r Report) Summary(colour bool) string {
	p := painter{on: colour}
	parts := []string{
		p.paint(ansiGreen, fmt.Sprintf("%d clean", r.Totals.Clean)),
	}
	if r.Totals.Flagged > 0 {
		parts = append(parts, p.paint(ansiRed+ansiBold, fmt.Sprintf("%d flagged", r.Totals.Flagged)))
	} else {
		parts = append(parts, "0 flagged")
	}
	if r.Totals.Errors > 0 {
		parts = append(parts, p.paint(ansiYellow, fmt.Sprintf("%d errors", r.Totals.Errors)))
	}
	if r.Totals.NoOracle > 0 {
		parts = append(parts, p.paint(ansiGrey, fmt.Sprintf("%d no-oracle", r.Totals.NoOracle)))
	}
	if r.Totals.Unsupported > 0 {
		parts = append(parts, p.paint(ansiGrey, fmt.Sprintf("%d unsupported", r.Totals.Unsupported)))
	}
	return strings.Join(parts, " · ")
}

// Markdown renders flagged tasks as a report ready to paste into an upstream
// issue: identity, the two scores, and the evidence behind the claim.
func (r Report) Markdown(w io.Writer) {
	fmt.Fprintf(w, "## Skeptic report\n\n")
	fmt.Fprintf(w, "`skeptic %s` — %s — %s/%s",
		r.SkepticVersion, r.GeneratedAt.Format("2006-01-02 15:04 MST"), r.Host.OS, r.Host.Arch)
	if r.Host.DockerAPI != "" {
		fmt.Fprintf(w, " — Docker API %s", r.Host.DockerAPI)
	}
	fmt.Fprintf(w, "\n\n%s\n\n", r.Summary(false))

	var flagged []TaskReport
	for _, t := range r.Tasks {
		if check.Verdict(t.Verdict).Flagged() {
			flagged = append(flagged, t)
		}
	}
	if len(flagged) == 0 {
		fmt.Fprintln(w, "No tasks were flagged. Every task's reference solution scored 1.0 and every task scored 0.0 with no changes applied.")
		return
	}

	fmt.Fprintln(w, "| Task | Verdict | nop | oracle |")
	fmt.Fprintln(w, "| --- | --- | --- | --- |")
	for _, t := range flagged {
		fmt.Fprintf(w, "| `%s` | **%s** | %s | %s |\n",
			t.ID, t.Verdict, score(t.NopScore), score(t.OracleScore))
	}

	for _, t := range flagged {
		fmt.Fprintf(w, "\n### `%s` — %s\n\n%s\n\n", t.ID, t.Verdict, explain(t))
		if t.Error != "" {
			fmt.Fprintf(w, "```\n%s\n```\n\n", t.Error)
		}
		for _, c := range []string{"nop", "oracle"} {
			out := tail(filepath.Join(t.LogDir, c, "test.stdout"), 30)
			if out == "" {
				continue
			}
			fmt.Fprintf(w, "<details>\n<summary>%s: last 30 lines of test output</summary>\n\n```\n%s\n```\n\n</details>\n\n", c, out)
		}
	}
}

// explain states, in prose, what the verdict means for this task.
func explain(t TaskReport) string {
	switch check.Verdict(t.Verdict) {
	case check.VerdictNopPasses:
		return fmt.Sprintf("The tests scored **%s** against an untouched workspace. "+
			"Nothing was changed, so this task's tests are not grading the change they are meant to grade.",
			score(t.NopScore))
	case check.VerdictOracleFails:
		return fmt.Sprintf("Applying the task's own reference solution scored **%s**, not 1.00. "+
			"If the known-correct answer cannot pass, no agent can.", score(t.OracleScore))
	case check.VerdictBoth:
		return fmt.Sprintf("Both controls failed: an untouched workspace scored **%s** (expected 0.00) "+
			"and the reference solution scored **%s** (expected 1.00).",
			score(t.NopScore), score(t.OracleScore))
	case check.VerdictError:
		return "This task could not be checked. It is reported separately so it is not miscounted as passing or failing."
	}
	return t.Reason
}

// tail returns the last n lines of a file, or "" if unreadable.
func tail(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func score(p *float64) string {
	if p == nil {
		return "–"
	}
	return fmt.Sprintf("%.2f", *p)
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
