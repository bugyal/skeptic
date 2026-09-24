// Package dockerfile parses Dockerfiles into instructions.
//
// It exists so the lint checks can reason about what a build actually does —
// which paths are copied, which values are set, which files a RUN writes —
// without invoking Docker or a model. The parser is deterministic by
// construction and covers the syntax real task Dockerfiles use: line
// continuations, heredocs, JSON-array instruction forms, shell quoting and
// build-arg flags.
package dockerfile

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Instruction is one parsed instruction.
type Instruction struct {
	// Cmd is the uppercased instruction name, e.g. COPY, RUN, ENV.
	Cmd string
	// Flags holds --name=value arguments such as --chown or --from.
	Flags []string
	// Args holds the non-flag arguments with quotes resolved. For a
	// shell-form RUN the single element is the whole command string.
	Args []string
	// Body is heredoc content for RUN <<EOF and COPY <<EOF instructions,
	// with the trailing newline stripped.
	Body       string
	HasHeredoc bool
	// Line is the 1-based line of the instruction in the source file.
	Line int
}

// File is a parsed Dockerfile.
type File struct {
	Instructions []Instruction
	// Path is the file the instructions came from.
	Path string
}

// ParsePath reads and parses the Dockerfile at path.
func ParsePath(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := Parse(string(b))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	f.Path = path
	return f, nil
}

// Parse parses Dockerfile text. It never fails on syntax it does not
// recognise; unparseable instructions are kept verbatim so a linter can still
// see them rather than silently skipping a line it did not understand.
func Parse(src string) (*File, error) {
	f := &File{}
	lines := strings.Split(src, "\n")

	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		trimmed := strings.TrimSpace(raw)
		// Comments and parser directives. A '#' is a comment only at the
		// start of a logical line; inside a continued RUN it is shell.
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Join continuation lines. A trailing backslash continues the
		// instruction unless it escapes a backslash itself, which shell
		// heredoc bodies never contain at line end in practice.
		logical := trimmed
		for strings.HasSuffix(logical, "\\") && !strings.HasSuffix(logical, "\\\\") && i+1 < len(lines) {
			i++
			logical = strings.TrimSuffix(logical, "\\") + " " + strings.TrimSpace(lines[i])
		}

		cmd, rest := splitCmd(logical)
		if cmd == "" {
			continue
		}

		in := Instruction{Cmd: cmd, Line: i + 1}

		// Heredoc redirections: RUN <<EOF ... EOF, COPY <<'EOF' /dest.
		if delims := heredocDelims(rest); len(delims) > 0 {
			in.HasHeredoc = true
			var body []string
			for i+1 < len(lines) {
				i++
				line := strings.TrimRight(lines[i], "\r")
				if containsDelim(delims, line) {
					// Only the last delimiter still pending closes the
					// instruction; multi-heredoc lines feed one body.
					delims = removeDelim(delims, line)
					if len(delims) == 0 {
						break
					}
					continue
				}
				body = append(body, line)
			}
			in.Body = strings.Join(body, "\n")
			rest = strings.TrimSpace(stripHeredocTokens(rest))
		}

		if isJSONForm(rest) {
			var args []string
			if err := json.Unmarshal([]byte(rest), &args); err != nil {
				// Not actually JSON after all; fall through to shell words
				// so the instruction is still visible to checks.
				in.Args = splitWords(rest)
			} else {
				in.Args = args
			}
		} else {
			in.Args = splitWords(rest)
		}

		in.Flags, in.Args = splitFlags(in.Args)
		f.Instructions = append(f.Instructions, in)
	}
	return f, nil
}

func splitCmd(s string) (cmd, rest string) {
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return strings.ToUpper(s), ""
	}
	return strings.ToUpper(s[:i]), strings.TrimSpace(s[i+1:])
}

// heredocDelims returns the closing delimiters declared by <<EOF-style
// redirections, with any quoting removed.
func heredocDelims(s string) []string {
	var out []string
	fields := strings.Fields(s)
	for _, f := range fields {
		// "<<<" is a here-string, not a heredoc.
		if strings.HasPrefix(f, "<<<") || !strings.HasPrefix(f, "<<") {
			continue
		}
		d := strings.Trim(strings.TrimPrefix(f, "<<"), `"'`)
		d = strings.TrimPrefix(d, "-")
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

func containsDelim(delims []string, line string) bool {
	for _, d := range delims {
		// <<- lets the closing delimiter carry leading whitespace.
		if line == d || strings.TrimSpace(line) == d {
			return true
		}
	}
	return false
}

func removeDelim(delims []string, line string) []string {
	out := delims[:0:0]
	removed := false
	for _, d := range delims {
		if !removed && d == line {
			removed = true
			continue
		}
		out = append(out, d)
	}
	return out
}

// stripHeredocTokens removes the <<EOF-style redirection tokens from an
// argument string while keeping the remaining arguments in place, so
// `COPY <<EOF /dest` still yields its destination and `RUN sh <<EOF` still
// runs sh.
func stripHeredocTokens(s string) string {
	var kept []string
	for _, f := range strings.Fields(s) {
		if strings.HasPrefix(f, "<<") {
			continue
		}
		kept = append(kept, f)
	}
	return strings.Join(kept, " ")
}

func isJSONForm(s string) bool {
	return strings.HasPrefix(s, "[")
}

// splitFlags separates --flag arguments from positional arguments.
func splitFlags(args []string) (flags, rest []string) {
	for _, a := range args {
		if strings.HasPrefix(a, "--") && len(a) > 2 {
			flags = append(flags, a)
			continue
		}
		rest = append(rest, a)
	}
	return flags, rest
}

// splitWords splits a command line into words the way shell would, removing
// one level of quoting. Backslash escapes outside quotes are honoured.
func splitWords(s string) []string {
	var (
		out    []string
		cur    strings.Builder
		inWord bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s):
			cur.WriteByte(s[i+1])
			i++
			inWord = true
		case c == '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				cur.WriteByte(c)
				inWord = true
				break
			}
			cur.WriteString(s[i+1 : i+1+end])
			i += end + 1
			inWord = true
		case c == '"':
			end := -1
			for j := i + 1; j < len(s); j++ {
				if s[j] == '\\' {
					j++
					continue
				}
				if s[j] == '"' {
					end = j
					break
				}
			}
			if end < 0 {
				cur.WriteByte(c)
				inWord = true
				break
			}
			cur.WriteString(unescapeDouble(s[i+1 : end]))
			i = end
			inWord = true
		case c == ' ' || c == '\t':
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out
}

func unescapeDouble(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case '"', '\\', '$', '`':
				b.WriteByte(s[i+1])
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// CopySources returns the host-side path arguments of every COPY and ADD, in
// order. Heredoc sources (COPY <<EOF) are content rather than a path and are
// excluded here; see HeredocCopies.
func (f *File) CopySources() []string {
	var out []string
	for _, in := range f.Instructions {
		if in.Cmd != "COPY" && in.Cmd != "ADD" {
			continue
		}
		if in.HasHeredoc {
			continue
		}
		// Last positional argument is the destination.
		if len(in.Args) > 1 {
			out = append(out, in.Args[:len(in.Args)-1]...)
		}
	}
	return out
}

// HeredocCopy pairs a COPY/ADD heredoc's content with its destination path.
type HeredocCopy struct {
	Dest string
	Body string
	Line int
}

// HeredocCopies returns every COPY/ADD whose source is inline content.
func (f *File) HeredocCopies() []HeredocCopy {
	var out []HeredocCopy
	for _, in := range f.Instructions {
		if (in.Cmd != "COPY" && in.Cmd != "ADD") || !in.HasHeredoc || len(in.Args) == 0 {
			continue
		}
		out = append(out, HeredocCopy{Dest: in.Args[len(in.Args)-1], Body: in.Body, Line: in.Line})
	}
	return out
}

// Runs returns every RUN instruction with its full command text: the shell
// string for shell form, the joined arguments for JSON form, plus any heredoc
// body on its own line so checks can scan both.
func (f *File) RunTexts() []string {
	var out []string
	for _, in := range f.Instructions {
		if in.Cmd != "RUN" {
			continue
		}
		cmd := strings.Join(in.Args, " ")
		if in.Body != "" {
			cmd += "\n" + in.Body
		}
		out = append(out, cmd)
	}
	return out
}

// EnvPairs returns ENV instructions as ordered key/value pairs. Both spellings
// are handled: the key=value form and the legacy `ENV key value` form, where
// everything after the key is the value.
func (f *File) EnvPairs() [][2]string {
	var out [][2]string
	for _, in := range f.Instructions {
		if in.Cmd != "ENV" || len(in.Args) == 0 {
			continue
		}
		if strings.Contains(in.Args[0], "=") {
			for _, a := range in.Args {
				k, v, ok := strings.Cut(a, "=")
				if ok {
					out = append(out, [2]string{k, v})
				}
			}
			continue
		}
		// Legacy form: `ENV key value words…`.
		out = append(out, [2]string{in.Args[0], strings.Join(in.Args[1:], " ")})
	}
	return out
}

// Line returns the source line of the first instruction with the given
// command, or 0.
func (f *File) Line(cmd string) int {
	for _, in := range f.Instructions {
		if in.Cmd == cmd {
			return in.Line
		}
	}
	return 0
}
