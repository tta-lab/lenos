package taskwarrior

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
)

// worktreeTaskIDRe matches the ttal worktree convention
//
//	<anything>/worktrees/<hex8>(-<alias>)?
//
// and captures the 8-hex task ID. Worktrees are checked out by ttal at this
// shape; the basename always carries the hex.
var worktreeTaskIDRe = regexp.MustCompile(`^([0-9a-f]{8})(?:-.+)?$`)

// ResolveTaskID derives the taskwarrior parent-task hex for the current
// process. Walks the cwd basename for the worktree convention; returns ""
// when the cwd is not a ttal worktree (e.g. running lenos in a regular
// project root).
//
// Callers should pass os.Getwd()'s result. Empty cwd is supported and
// returns "".
func ResolveTaskID(cwd string) string {
	if cwd == "" {
		return ""
	}
	base := filepath.Base(cwd)
	if m := worktreeTaskIDRe.FindStringSubmatch(base); len(m) >= 2 {
		return m[1]
	}
	return ""
}

// ResolveTaskIDFromCwd is a convenience wrapper that calls os.Getwd
// internally. Returns "" on any error — and logs the Getwd failure so
// callers diagnosing "not a worktree" don't chase the wrong root cause
// (a deleted cwd looks identical to a non-worktree path otherwise).
func ResolveTaskIDFromCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		slog.Warn("taskwarrior.ResolveTaskIDFromCwd: getwd failed", "err", err)
		return ""
	}
	return ResolveTaskID(cwd)
}
