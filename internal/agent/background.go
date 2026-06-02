package agent

// BackgroundJob is a placeholder kept for UI compatibility. The SDK sandbox
// is synchronous — background jobs are no longer created.
type BackgroundJob struct {
	ID      string
	Command string
}
