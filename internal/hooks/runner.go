package hooks

import "context"

// Runner runs a hook with the given JSON payload on stdin. Implementations must
// return promptly when ctx is canceled. Errors are non-fatal at the call site —
// the loop never aborts on hook failure — but the call site logs them.
type Runner interface {
	Run(ctx context.Context, payload []byte) error
}

// NoopRunner is a Runner that does nothing. Tests use this when wiring should
// be exercised without spawning subprocesses.
type NoopRunner struct{}

func (NoopRunner) Run(context.Context, []byte) error { return nil }
