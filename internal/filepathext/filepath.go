package filepathext

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// SmartJoin joins two paths, treating the second path as absolute if it is an
// absolute path.
func SmartJoin(one, two string) string {
	if SmartIsAbs(two) {
		return two
	}
	return filepath.Join(one, two)
}

// SmartIsAbs checks if a path is absolute, considering both OS-specific and
// Unix-style paths.
func SmartIsAbs(path string) bool {
	switch runtime.GOOS {
	case "windows":
		return filepath.IsAbs(path) || strings.HasPrefix(filepath.ToSlash(path), "/")
	default:
		return filepath.IsAbs(path)
	}
}

// ContainedJoin resolves userPath against workingDir and guarantees the
// result stays inside workingDir.
//
// The workingDir root is canonicalized via EvalSymlinks so symlinked roots
// (e.g. macOS /tmp → /private/tmp) don't cause false rejections. Absolute
// userPaths are also EvalSymlinks-resolved when they exist on disk, so an
// agent passing /tmp/session/file.go when the canonical root is
// /private/tmp/session still works. Relative userPaths are resolved
// lexically against the canonical root — no EvalSymlinks on user content,
// which means a symlink *inside* workingDir pointing outside is NOT
// detected. That's a deliberate scoping decision: this helper closes the
// common case (accidental or injected out-of-worktree writes) without
// paying the cost of per-write filesystem walks or fighting TOCTOU.
func ContainedJoin(workingDir, userPath string) (string, error) {
	root, err := filepath.EvalSymlinks(workingDir)
	if err != nil {
		root = filepath.Clean(workingDir)
	}

	var joined string
	if SmartIsAbs(userPath) {
		// Try to resolve symlinks in the directory component; fall back to
		// cleaning the full path if the file doesn't exist yet.
		dir, base := filepath.Dir(userPath), filepath.Base(userPath)
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			joined = filepath.Join(resolved, base)
		} else {
			joined = filepath.Clean(userPath)
		}
	} else {
		joined = filepath.Clean(filepath.Join(root, userPath))
	}

	rel, err := filepath.Rel(root, joined)
	if err != nil {
		return "", fmt.Errorf("path %q is outside working directory %q", userPath, root)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes working directory %q", userPath, root)
	}
	return joined, nil
}
