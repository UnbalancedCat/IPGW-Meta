package cli

import (
	"io"
	"os"

	ipgw "github.com/UnbalancedCat/ipgw-meta"
	"golang.org/x/term"
)

// IsSafeTerminal reports whether both the input and the diagnostic/QR output
// are attached to real terminals. A terminal on only one side is not safe for
// an interactive authentication prompt.
func IsSafeTerminal(stdin, stderr *os.File) bool {
	return isSafeTerminal(stdin, stderr, term.IsTerminal)
}

func isSafeTerminal(stdin, stderr *os.File, checker func(int) bool) bool {
	if stdin == nil || stderr == nil || checker == nil {
		return false
	}
	return checker(int(stdin.Fd())) && checker(int(stderr.Fd()))
}

// StartupFailure renders a failure that happened before the normal command
// runtime could be constructed. The cause is deliberately not retained: it
// may contain a local path or other untrusted diagnostic material, and even a
// context sentinel must not change this startup/config failure into a network
// or cancellation result.
func StartupFailure(args []string, out, err io.Writer, cause error) int {
	_ = cause
	if out == nil {
		out = io.Discard
	}
	if err == nil {
		err = io.Discard
	}
	render := renderer{mode: preparseOutputMode(args), out: out, err: err}
	return render.failure("cli", &ipgw.Error{
		Code:      ipgw.CodeConfig,
		Message:   "configuration error",
		Retryable: false,
	})
}
