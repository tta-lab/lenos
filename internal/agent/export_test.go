// Package agent test seams. This file is _test.go so it's only compiled with tests.
package agent

import "time"

// SetHookTimeout lets tests shrink the post_step hook timeout. Returns a restore
// func — call via defer to ensure cleanup even on test failure.
func SetHookTimeout(d time.Duration) (restore func()) {
	prev := hookTimeout
	hookTimeout = d
	return func() { hookTimeout = prev }
}
