package tui

import (
	"os"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// MdAppendedMsg is emitted when the file grows; contains the new bytes.
type MdAppendedMsg struct{ Bytes []byte }

// MdTruncatedMsg is emitted when the file shrinks (e.g. session reset).
type MdTruncatedMsg struct{}

// MdWatchErrMsg is emitted on fsnotify errors.
type MdWatchErrMsg struct{ Err error }

// Watcher tails a .md file and emits tea.Msg on changes.
type Watcher struct {
	path    string
	offset  int64
	watcher *fsnotify.Watcher
	events  chan event
	mu      sync.Mutex
	running bool
}

// event is the internal event type.
type event struct {
	appended  []byte
	truncated bool
	err       error
}

// NewWatcher opens the .md file, reads current content, and starts watching.
// Returns the initial bytes (everything in the file at construction time)
// plus a Watcher whose Listen() method returns a tea.Cmd for Bubble Tea.
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
		path:    path,
		offset:  int64(len(content)),
		watcher: watcher,
		events:  make(chan event, 32),
		running: true,
	}

	// Start the background tail loop.
	go w.tailLoop(debounce)

	return content, w, nil
}

// tailLoop watches for fsnotify events and coalesces writes within debounce window.
func (w *Watcher) tailLoop(debounce time.Duration) {
	var timer *time.Timer
	for w.running {
		select {
		case err := <-w.watcher.Errors:
			w.events <- event{err: err}

		case e := <-w.watcher.Events:
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
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(debounce, func() {
					w.readAppend()
				})
			}
		}
	}
}

// readAppend reads new bytes from the current offset and emits MdAppendedMsg.
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

// Listen returns a tea.Cmd that blocks until the next file event.
func (w *Watcher) Listen() func() tea.Msg {
	return func() tea.Msg {
		e := <-w.events
		if e.err != nil {
			return MdWatchErrMsg{Err: e.err}
		}
		if e.truncated {
			return MdTruncatedMsg{}
		}
		return MdAppendedMsg{Bytes: e.appended}
	}
}

// Close releases fsnotify resources.
func (w *Watcher) Close() error {
	w.mu.Lock()
	w.running = false
	w.mu.Unlock()
	return w.watcher.Close()
}
