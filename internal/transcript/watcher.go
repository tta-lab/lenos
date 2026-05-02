package transcript

import (
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatchEvent is the marker interface for events delivered by Watcher.Next.
// Concrete types are WatchAppend, WatchTruncate, WatchError.
type WatchEvent interface{ isWatchEvent() }

// WatchAppend is delivered when the file grew; Bytes carries the new tail.
type WatchAppend struct{ Bytes []byte }

// WatchTruncate is delivered when the file shrank (e.g. session reset).
type WatchTruncate struct{}

// WatchError is delivered when fsnotify reports an error.
type WatchError struct{ Err error }

func (WatchAppend) isWatchEvent()   {}
func (WatchTruncate) isWatchEvent() {}
func (WatchError) isWatchEvent()    {}

// Watcher tails a .md file and emits WatchEvents on Next.
type Watcher struct {
	path     string
	offset   int64
	watcher  *fsnotify.Watcher
	events   chan event
	closeSig chan struct{}
	mu       sync.Mutex
	closed   bool
	timer    *time.Timer // stored for Close to stop it
}

// event is the internal event type.
type event struct {
	appended  []byte
	truncated bool
	err       error
}

// NewWatcher opens the .md file, reads current content, and starts watching.
// Returns the initial bytes (everything in the file at construction time)
// plus a Watcher whose Next() method blocks until the next file event.
// Callers wanting Bubble Tea integration wrap Next in a tea.Cmd one-liner.
func NewWatcher(path string, debounce time.Duration) (initial []byte, w *Watcher, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, nil, err
	}

	if err := watcher.Add(path); err != nil {
		watcher.Close()
		return nil, nil, err
	}

	w = &Watcher{
		path:     path,
		offset:   int64(len(content)),
		watcher:  watcher,
		events:   make(chan event, 32),
		closeSig: make(chan struct{}),
	}

	// Start the background tail loop.
	go w.tailLoop(debounce)

	return content, w, nil
}

// tailLoop watches for fsnotify events and coalesces writes within debounce window.
func (w *Watcher) tailLoop(debounce time.Duration) {
	for {
		select {
		case err := <-w.watcher.Errors:
			w.events <- event{err: err}

		case <-w.closeSig:
			// Watcher closed (via Close()); exit cleanly.
			return

		case e := <-w.watcher.Events:
			w.mu.Lock()
			closed := w.closed
			w.mu.Unlock()
			if closed {
				continue
			}

			if e.Has(fsnotify.Remove) || e.Has(fsnotify.Rename) {
				// File was deleted or moved; re-watch if it reappears.
				w.watcher.Remove(w.path)
				_ = w.watcher.Add(w.path)
				// Treat as truncation — next Write will re-open and read from 0.
				select {
				case w.events <- event{truncated: true}:
				default:
				}
				continue
			}
			if e.Has(fsnotify.Write) {
				// Debounce: reset timer on each write event.
				w.mu.Lock()
				if w.timer != nil {
					w.timer.Stop()
				}
				w.timer = time.AfterFunc(debounce, func() {
					w.readAppend()
				})
				w.mu.Unlock()
			}
		}
	}
}

// readAppend reads new bytes from the current offset and emits WatchAppend.
func (w *Watcher) readAppend() {
	w.mu.Lock()
	defer w.mu.Unlock()

	f, err := os.Open(w.path)
	if err != nil {
		w.events <- event{err: err}
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		w.events <- event{err: err}
		return
	}

	if info.Size() < w.offset {
		// Truncation.
		w.offset = 0
		if _, err := f.Seek(0, 0); err != nil {
			w.events <- event{err: err}
			return
		}
		// Read everything from the start.
		content, err := os.ReadFile(w.path)
		if err != nil {
			w.events <- event{err: err}
			return
		}
		w.offset = info.Size()
		w.events <- event{truncated: true, appended: content}
		return
	}

	// Append from current offset.
	if info.Size() == w.offset {
		return // no new content
	}

	if _, err := f.Seek(w.offset, 0); err != nil {
		w.events <- event{err: err}
		return
	}

	newBytes := make([]byte, info.Size()-w.offset)
	n, err := f.Read(newBytes)
	if err != nil {
		w.events <- event{err: err}
		return
	}

	w.offset += int64(n)
	w.events <- event{appended: newBytes[:n]}
}

// Next blocks until the next file event and returns it. Tea-aware callers
// wrap this in `func() tea.Msg { return w.Next() }`.
func (w *Watcher) Next() WatchEvent {
	e := <-w.events
	if e.err != nil {
		return WatchError{Err: e.err}
	}
	if e.truncated {
		return WatchTruncate{}
	}
	return WatchAppend{Bytes: e.appended}
}

// Close releases fsnotify resources and stops the background goroutine promptly.
func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	if w.timer != nil {
		w.timer.Stop()
	}
	// Capture references while holding the lock.
	closeSig := w.closeSig
	watcher := w.watcher
	w.mu.Unlock()

	close(closeSig)
	return watcher.Close()
}
