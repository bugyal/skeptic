package dockerfile

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) *File {
	t.Helper()
	f, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return f
}

func TestCopySources(t *testing.T) {
	f := mustParse(t, strings.Join([]string{
		"FROM alpine:3",
		"COPY . /app",
		`COPY --chown=1000:1000 "src dir" /app/src`,
		"COPY --from=build /out/bin /usr/local/bin/",
		"ADD https://example.com/f.tar.gz /tmp/",
	}, "\n"))

	want := []string{".", "src dir", "/out/bin", "https://example.com/f.tar.gz"}
	got := f.CopySources()
	if len(got) != len(want) {
		t.Fatalf("CopySources = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("CopySources[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// Flags must never be mistaken for source paths: COPY --from=builder would
// otherwise look like a copy of a directory literally named "--from=builder".
func TestCopyFlagsDropped(t *testing.T) {
	f := mustParse(t, "COPY --from=builder --chown=root /x /y")
	if got := f.CopySources(); len(got) != 1 || got[0] != "/x" {
		t.Fatalf("CopySources = %q, want [/x]", got)
	}
	if len(f.Instructions[0].Flags) != 2 {
		t.Fatalf("flags = %q, want 2 entries", f.Instructions[0].Flags)
	}
}

// A COPY with only a destination (heredoc content excluded elsewhere) must not
// yield a bogus source.
func TestCopySingleArgHasNoSource(t *testing.T) {
	f := mustParse(t, "COPY /dest")
	if got := f.CopySources(); len(got) != 0 {
		t.Fatalf("CopySources = %q, want none", got)
	}
}

func TestLineContinuation(t *testing.T) {
	f := mustParse(t, "RUN apt-get update \\\n\t&& apt-get install -y curl \\\n\t&& rm -rf /var/lib/apt/lists/*")
	if len(f.Instructions) != 1 {
		t.Fatalf("got %d instructions, want 1", len(f.Instructions))
	}
	text := f.RunTexts()[0]
	for _, part := range []string{"apt-get update", "apt-get install -y curl", "rm -rf /var/lib/apt/lists/*"} {
		if !strings.Contains(text, part) {
			t.Errorf("joined RUN missing %q: %q", part, text)
		}
	}
}

// A backslash that escapes itself is not a continuation; dropping this rule
// would merge two separate lines and hide one of them from checks.
func TestEscapedBackslashNotContinuation(t *testing.T) {
	f := mustParse(t, "RUN echo 'a\\\\'\nRUN echo second")
	if len(f.Instructions) != 2 {
		t.Fatalf("got %d instructions, want 2", len(f.Instructions))
	}
}

func TestJSONFormRun(t *testing.T) {
	f := mustParse(t, `RUN ["echo", "hello world"]`)
	texts := f.RunTexts()
	if len(texts) != 1 || texts[0] != "echo hello world" {
		t.Fatalf("RunTexts = %q, want [echo hello world]", texts)
	}
}

func TestJSONFormCopy(t *testing.T) {
	f := mustParse(t, `COPY ["a b.txt", "/dest/"]`)
	if got := f.CopySources(); len(got) != 1 || got[0] != "a b.txt" {
		t.Fatalf("CopySources = %q, want [a b.txt]", got)
	}
}

func TestEnvPairsBothForms(t *testing.T) {
	f := mustParse(t, strings.Join([]string{
		"ENV A=1 B=\"two words\"",
		"ENV C three",
		"ENV D=4 E=5",
	}, "\n"))
	want := [][2]string{{"A", "1"}, {"B", "two words"}, {"C", "three"}, {"D", "4"}, {"E", "5"}}
	got := f.EnvPairs()
	if len(got) != len(want) {
		t.Fatalf("EnvPairs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("EnvPairs[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestHeredocCopy(t *testing.T) {
	f := mustParse(t, "COPY <<EOF /app/answer.txt\n42\nEOF\nRUN true")
	copies := f.HeredocCopies()
	if len(copies) != 1 {
		t.Fatalf("HeredocCopies = %d entries, want 1", len(copies))
	}
	if copies[0].Dest != "/app/answer.txt" || copies[0].Body != "42" || copies[0].Line != 1 {
		t.Fatalf("copy = %+v", copies[0])
	}
	// The heredoc content must not leak into the regular COPY sources, where
	// it would look like a path named "42".
	if got := f.CopySources(); len(got) != 0 {
		t.Errorf("CopySources = %q, want none for heredoc COPY", got)
	}
	if len(f.Instructions) != 2 {
		t.Errorf("parsed %d instructions, want 2 (heredoc body must not become instructions)", len(f.Instructions))
	}
}

// COPY <<'EOF' (quoted delimiter) and multiple heredocs on one instruction.
func TestHeredocQuotedAndMultiple(t *testing.T) {
	f := mustParse(t, "RUN <<'A' <<B\none\nA\ntwo\nB")
	if !f.Instructions[0].HasHeredoc {
		t.Fatal("HasHeredoc = false")
	}
	if body := f.Instructions[0].Body; body != "one\ntwo" {
		t.Fatalf("body = %q, want %q", body, "one\ntwo")
	}
}

// RUN <<EOF pipes the body to a shell; the text must be scannable.
func TestHeredocRun(t *testing.T) {
	f := mustParse(t, "RUN <<EOF\necho 42 > /app/answer.txt\nEOF")
	texts := f.RunTexts()
	if len(texts) != 1 || !strings.Contains(texts[0], "echo 42 > /app/answer.txt") {
		t.Fatalf("RunTexts = %q", texts)
	}
}

// <<- strips leading tabs in shell, but the delimiter itself is still the
// closer; the parser only needs to find it.
func TestHeredocDashDelimiter(t *testing.T) {
	f := mustParse(t, "COPY <<-EOF /dest\n\t42\n\tEOF")
	if got := f.HeredocCopies(); len(got) != 1 || got[0].Body != "\t42" {
		t.Fatalf("HeredocCopies = %+v", got)
	}
}

// <<< is a here-string, not a heredoc; treating it as one would swallow the
// rest of the Dockerfile as instruction body.
func TestHereStringIsNotHeredoc(t *testing.T) {
	f := mustParse(t, "RUN echo <<< \"$HOME\" >/dev/null\nRUN true")
	if len(f.Instructions) != 2 {
		t.Fatalf("got %d instructions, want 2", len(f.Instructions))
	}
	for _, in := range f.Instructions {
		if in.HasHeredoc {
			t.Errorf("%s: HasHeredoc = true, want false", in.Cmd)
		}
	}
}

// The redirection may sit mid-command; remaining words must survive.
func TestHeredocMidCommand(t *testing.T) {
	f := mustParse(t, "RUN sh -c 'cat' <<EOF /dev/null\n42\nEOF")
	if got := f.Instructions[0].Args; len(got) == 0 || got[0] != "sh" {
		t.Fatalf("Args = %q, want sh first", got)
	}
}

func TestCommentsAndBlanks(t *testing.T) {
	f := mustParse(t, "# syntax=docker/dockerfile:1\n\nFROM alpine:3\n# a comment\nRUN true")
	if len(f.Instructions) != 2 {
		t.Fatalf("got %d instructions, want 2", len(f.Instructions))
	}
	if f.Instructions[0].Cmd != "FROM" || f.Instructions[1].Cmd != "RUN" {
		t.Fatalf("instructions = %s, %s", f.Instructions[0].Cmd, f.Instructions[1].Cmd)
	}
}

// A '#' after a continuation belongs to the shell command, not to the parser.
func TestHashInsideContinuation(t *testing.T) {
	f := mustParse(t, "RUN echo one \\\n  # not a comment")
	if len(f.Instructions) != 1 {
		t.Fatalf("got %d instructions, want 1", len(f.Instructions))
	}
	if text := f.RunTexts()[0]; !strings.Contains(text, "# not a comment") {
		t.Fatalf("RUN text = %q, want the hash line kept", text)
	}
}

func TestLineNumbers(t *testing.T) {
	f := mustParse(t, "FROM alpine:3\n\nRUN true\nCOPY . /app")
	if f.Instructions[0].Line != 1 || f.Instructions[1].Line != 3 || f.Instructions[2].Line != 4 {
		t.Fatalf("lines = %d, %d, %d", f.Instructions[0].Line, f.Instructions[1].Line, f.Instructions[2].Line)
	}
}

func TestQuotingStripped(t *testing.T) {
	f := mustParse(t, `RUN echo "answer is 42" > /app/a.txt`)
	text := f.RunTexts()[0]
	if !strings.Contains(text, `answer is 42`) {
		t.Fatalf("RunTexts = %q, want unquoted content", text)
	}
}

// Unknown or malformed instructions are kept, not dropped: a linter must see
// everything the file says.
func TestUnrecognisedKept(t *testing.T) {
	f := mustParse(t, "HEALTHCHECK CMD curl -f http://localhost || exit 1\nFROM alpine:3")
	if len(f.Instructions) != 2 || f.Instructions[0].Cmd != "HEALTHCHECK" {
		t.Fatalf("instructions = %+v", f.Instructions)
	}
}

func TestParseErrorOnMissingFile(t *testing.T) {
	if _, err := ParsePath(t.TempDir() + "/nope"); err == nil {
		t.Fatal("ParsePath on missing file should error")
	}
}
