package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tta-lab/lenos/internal/fsext"
	"github.com/tta-lab/lenos/internal/transcript"
)

// resolveSessionPath returns the absolute .md path.
//
// Resolution rules — cwd is the single source of truth:
//   - LENOS_SESSION_ID is the only required env (a session ID can't be
//     auto-derived without a state file; multiple session.md files can
//     coexist in the same data dir).
//   - The data directory is found via fsext.LookupClosest from cwd,
//     matching how lenos's main process resolves DataDirectory. When no
//     ancestor .lenos/ exists, falls back to <cwd>/.lenos.
func resolveSessionPath() (string, error) {
	sessionID := os.Getenv("LENOS_SESSION_ID")
	if sessionID == "" {
		return "", errors.New("LENOS_SESSION_ID not set; this binary is for use inside a lenos session")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("read cwd: %w", err)
	}
	dataDir, ok := fsext.LookupClosest(cwd, ".lenos")
	if !ok {
		dataDir = filepath.Join(cwd, ".lenos")
	}
	return filepath.Join(dataDir, "sessions", sessionID+".md"), nil
}

// resolveInput picks the message body from positional args first, falling
// back to piped stdin only when no args are given. Args-first prevents
// blocking on pueue/systemd-inherited unused stdin pipes (see ttal send's
// resolveSendMessage for the same pattern).
func resolveInput(args []string, stdin io.Reader) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	piped, err := readStdinIfPiped(stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	if piped == "" {
		return "", errors.New("content required (positional argument or piped stdin)")
	}
	return piped, nil
}

// readStdinIfPiped returns stdin contents IF stdin is a piped/redirected fd.
// Returns "" with no error when stdin is a TTY (interactive shell). Mirrors
// ttal send's pattern for handling inherited-but-unused pipes.
func readStdinIfPiped(stdin io.Reader) (string, error) {
	f, ok := stdin.(*os.File)
	if !ok {
		// Test injection — read whatever was passed.
		b, err := io.ReadAll(stdin)
		return string(b), err
	}
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if (info.Mode() & os.ModeCharDevice) != 0 {
		// TTY — don't block.
		return "", nil
	}
	b, err := io.ReadAll(f)
	return string(b), err
}

const (
	appendMaxRetries = 3
	appendRetryDelay = 10 * time.Millisecond
)

// appendWithRetry calls AppendStrict and retries up to appendMaxRetries times
// on ErrConcurrentWrite, with appendRetryDelay backoff between attempts.
// All other errors return immediately (no retry).
func appendWithRetry(w *transcript.MdWriter, p []byte) error {
	var err error
	for i := 0; i < appendMaxRetries; i++ {
		err = w.AppendStrict(p)
		if err == nil {
			return nil
		}
		if !errors.Is(err, transcript.ErrConcurrentWrite) {
			return err
		}
		time.Sleep(appendRetryDelay)
	}
	return fmt.Errorf("lock contention after %d attempts: %w", appendMaxRetries, err)
}
