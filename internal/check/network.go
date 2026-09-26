package check

import "strings"

// networkSignatures are messages that only appear when a process could not
// reach the network: DNS that did not resolve, a route that does not exist,
// TLS that an intercepting proxy broke. Each is the runtime's own wording, not
// prose a test author would write, and all are case-sensitive, so a test named
// test_certificate_verify_failed or test_proxy_error does not match.
//
// Deliberately absent: "Connection refused" and "timed out". Test suites start
// local servers and exercise timeouts on purpose, and both appear in healthy
// output often enough that matching them would turn real failures into ERROR.
var networkSignatures = []string{
	"CERTIFICATE_VERIFY_FAILED",            // OpenSSL, via Python's ssl module
	"Temporary failure in name resolution", // glibc EAI_AGAIN
	"Name or service not known",            // glibc EAI_NONAME
	"nodename nor servname provided",       // BSD/macOS resolver
	"getaddrinfo ENOTFOUND",                // Node.js
	"getaddrinfo EAI_AGAIN",                // Node.js
	"Network is unreachable",               // ENETUNREACH
	"No route to host",                     // EHOSTUNREACH
	"Could not resolve host",               // curl
	"requests.exceptions.ProxyError",       // requests, behind a refusing proxy
}

// networkFailure returns the last line of output carrying a network-failure
// signature, or "" when there is none. The last, because harnesses install
// things before they test: the first match is often a package manager in
// setup, while the last is nearer the test that actually failed, which is the
// line a reader needs to see.
//
// It is consulted only after a control has already scored badly, to decide
// whether that score was earned. It never raises a score and never clears a
// flag into CLEAN: at most it turns a flag into ERROR, which says the task
// could not be checked on this host. See docs/decisions.md D16.
func networkFailure(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		for _, sig := range networkSignatures {
			if strings.Contains(lines[i], sig) {
				line := strings.TrimSpace(lines[i])
				if len(line) > 200 {
					line = line[:200] + "…"
				}
				return line
			}
		}
	}
	return ""
}
