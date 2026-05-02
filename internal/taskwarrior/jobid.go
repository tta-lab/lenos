package taskwarrior

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
)

// worktreeJobIDRe matches the ttal worktree convention
//
//	<anything>/worktrees/<hex8>(-<alias>)?
//
// and captures the 8-hex job ID. Worktrees are checked out by ttal at this
// shape; the basename always carries the hex.
var worktreeJobIDRe = regexp.MustCompile(`^([0-9a-f]{8})(?:-.+)?$`)

// ResolveJobID derives the taskwarrior parent-task hex for the current
// process. Walks the cwd basename for the worktree convention; returns ""
// when the cwd is not a ttal worktree (e.g. running lenos in a regular
// project root).
//
// Callers should pass os.Getwd()'s result. Empty cwd is supported and
// returns "".
func ResolveJobID(cwd string) string {
	if cwd == "" {
		return ""
	}
	base := filepath.Base(cwd)
	if m := worktreeJobIDRe.FindStringSubmatch(base); len(m) >= 2 {
		return m[1]
	}
	return ""
}

// ResolveJobIDFromCwd is a convenience wrapper that calls os.Getwd
// internally. Returns "" on any error — and logs the Getwd failure so
// callers diagnosing "not a worktree" don't chase the wrong root cause
// (a deleted cwd looks identical to a non-worktree path otherwise).
func ResolveJobIDFromCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		slog.Warn("taskwarrior.ResolveJobIDFromCwd: getwd failed", "err", err)
		return ""
	}
	return ResolveJobID(cwd)
}
