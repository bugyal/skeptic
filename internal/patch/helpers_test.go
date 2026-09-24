package patch

import "os"

// Without0 is a thin wrapper so tests read clearly.
func Without0(p *Patch, i int) (*Patch, Hunk, error) { return p.Without(i) }

func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o644) }
