package tui

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tta-lab/lenos/internal/agent/notify"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/pubsub"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/ui/common"
	"github.com/tta-lab/lenos/internal/ui/notification"
	uistyles "github.com/tta-lab/lenos/internal/ui/styles"
	"github.com/tta-lab/lenos/internal/workspace"
)

// stubWorkspace embeds an unimplemented workspace.Workspace; only the methods
// exercised by tests are overridden. Anything else panics, which keeps tests
// honest about the surface they touch.
type stubWorkspace struct {
	workspace.Workspace

	mu             sync.Mutex
	cfg            *config.Config
	sessions       []session.Session
	getSessionByID map[string]session.Session
	createSession  session.Session
	queueDepth     int
	queueItems     []string
	gitWorktree    bool
	modifiedFiles  []workspace.ModifiedFile
	listFilesErr   error
	agentReady     bool
	agentRunCalls  []string
	sandboxState   string
	model          workspace.AgentModel
	agentName      string
}

func (s *stubWorkspace) Config() *config.Config { return s.cfg }
func (s *stubWorkspace) ListSessions(ctx context.Context) ([]session.Session, error) {
	return s.sessions, nil
}

func (s *stubWorkspace) GetSession(ctx context.Context, id string) (session.Session, error) {
	if got, ok := s.getSessionByID[id]; ok {
		return got, nil
	}
	return session.Session{ID: id}, nil
}

func (s *stubWorkspace) CreateSession(ctx context.Context, title string) (session.Session, error) {
	return s.createSession, nil
}
func (s *stubWorkspace) AgentQueuedPrompts(sessionID string) int { return s.queueDepth }
func (s *stubWorkspace) AgentQueuedPromptsList(sessionID string) []string {
	return s.queueItems
}
func (s *stubWorkspace) IsGitWorktree(ctx context.Context) bool { return s.gitWorktree }
func (s *stubWorkspace) ListModifiedFiles(ctx context.Context) ([]workspace.ModifiedFile, error) {
	return s.modifiedFiles, s.listFilesErr
}
func (s *stubWorkspace) AgentIsReady() bool { return s.agentReady }
func (s *stubWorkspace) AgentRun(ctx context.Context, sessionID, prompt string, _ ...message.Attachment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentRunCalls = append(s.agentRunCalls, sessionID+":"+prompt)
	return nil
}
func (s *stubWorkspace) AgentSandboxState() string        { return s.sandboxState }
func (s *stubWorkspace) AgentModel() workspace.AgentModel { return s.model }
func (s *stubWorkspace) AgentName() string                { return s.agentName }

func newStubCommon(ws workspace.Workspace) *common.Common {
	st := uistyles.DefaultStyles()
	return &common.Common{Workspace: ws, Styles: &st}
}

// newTestApp builds an App pointing at the given session .md fixture path. It
// pokes mdPath/sessionID directly to bypass DataDirectory derivation — tests
// that need full New() coverage should construct a stubWorkspace whose Config
// surfaces a temp directory containing sessions/<id>.md.
func newTestApp(t *testing.T, sessionID, mdPath string, ws workspace.Workspace) *App {
	t.Helper()
	com := newStubCommon(ws)
	t.Setenv("TTAL_JOB_ID", "")
	a := &App{
		com:       com,
		mdPath:    mdPath,
		sessionID: sessionID,
		styles:    NewStyles(),
		keys:      DefaultKeyMap(),
		viewport:  NewViewport(100, 100, NewStyles()),
		notify:    NewNotificationDispatcher(nil),
	}
	a.header = NewHeader(ws, Frontmatter{}, a.styles)
	a.footer = NewFooter(a.styles)
	a.bottomBar = NewBottomBar(a.styles, com.Styles)
	a.input = newInputPane()
	a.sess = &session.Session{ID: sessionID}
	return a
}

// fullSessionFixture is the markdown fixture used across the legacy tests.
var fullSessionFixture = filepath.Join("..", "transcript", "testdata", "full_session.md")

// --- Test 1: New() emits a non-empty Init batch ---

func TestApp_InitEmitsBatchWithSubscriptions(t *testing.T) {
	t.Setenv("TTAL_JOB_ID", "")
	ws := &stubWorkspace{
		cfg: &config.Config{Options: &config.Options{DataDirectory: t.TempDir()}},
	}
	a := New(newStubCommon(ws), "sess-1", false, "")
	require.NotNil(t, a)
	require.NotNil(t, a.Init(), "Init must produce at least Tick + watcher")
}

// --- Tests 2/3: TTAL_JOB_ID gating ---

func TestApp_InitStartsTwPollWhenJobIDSet(t *testing.T) {
	t.Setenv("TTAL_JOB_ID", "job-abc")
	ws := &stubWorkspace{
		cfg: &config.Config{Options: &config.Options{DataDirectory: t.TempDir()}},
	}
	a := New(newStubCommon(ws), "sess-1", false, "")
	defer func() {
		if a.twPoller != nil {
			a.twPoller.Stop()
		}
	}()
	require.NotNil(t, a.twPoller, "TTAL_JOB_ID set → TwPoller wired")
}

func TestApp_InitSkipsTwPollWhenJobIDEmpty(t *testing.T) {
	t.Setenv("TTAL_JOB_ID", "")
	ws := &stubWorkspace{
		cfg: &config.Config{Options: &config.Options{DataDirectory: t.TempDir()}},
	}
	a := New(newStubCommon(ws), "sess-1", false, "")
	require.Nil(t, a.twPoller, "no TTAL_JOB_ID → TwPoller is nil")
}

// --- Test 4: window-size routing ---

func TestApp_WindowSizePropagatesToSubcomponents(t *testing.T) {
	a := newTestApp(t, "sess-1", fullSessionFixture, &stubWorkspace{})
	a.md = []byte("hello\n")

	m, _ := a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a = m.(*App)
	assert.Equal(t, 120, a.width)
	assert.Equal(t, 40, a.height)
	assert.Equal(t, 120, a.header.width)
	assert.Equal(t, 120, a.footer.width)
	assert.Equal(t, 120, a.bottomBar.width)
}

// --- Test 5: ctrl+d toggles header ---

func TestApp_CtrlDRoutesToHeaderToggle(t *testing.T) {
	a := newTestApp(t, "sess-1", "", &stubWorkspace{})
	a.width = 100
	require.True(t, a.header.IsCompact())

	a.Update(keyPress("ctrl+d"))
	assert.False(t, a.header.IsCompact(), "ctrl+d toggles header to expanded")

	a.Update(keyPress("ctrl+d"))
	assert.True(t, a.header.IsCompact(), "ctrl+d toggles header back to compact")
}

// --- Test 6: ctrl+t toggles bottomBar ---

func TestApp_CtrlTRoutesToBottomBarToggle(t *testing.T) {
	a := newTestApp(t, "sess-1", "", &stubWorkspace{})
	a.width = 100
	require.True(t, a.bottomBar.IsCompact())

	a.Update(keyPress("ctrl+t"))
	assert.False(t, a.bottomBar.IsCompact())

	a.Update(keyPress("ctrl+t"))
	assert.True(t, a.bottomBar.IsCompact())
}

// --- Test 7: Tick refreshes queue from workspace ---

func TestApp_TickRefreshesQueueFromWorkspace(t *testing.T) {
	ws := &stubWorkspace{queueDepth: 4, queueItems: []string{"a", "b", "c", "d"}}
	a := newTestApp(t, "sess-1", "", ws)
	a.width = 100

	a.Update(TickMsg{})
	assert.Equal(t, 4, a.bottomBar.queueDepth)
	assert.Equal(t, []string{"a", "b", "c", "d"}, a.bottomBar.queueItems)
}

// --- Test 8: TwPollMsg sets header todos only ---

func TestApp_TwPollMsgRoutesToHeaderSetTodosOnly(t *testing.T) {
	a := newTestApp(t, "sess-1", "", &stubWorkspace{})
	beforeQueueDepth := a.bottomBar.queueDepth

	todos := []session.Todo{{Content: "first", Status: session.TodoStatusPending}}
	a.Update(TwPollMsg{Todos: todos})

	assert.Equal(t, todos, a.header.todos)
	assert.Equal(t, beforeQueueDepth, a.bottomBar.queueDepth, "TwPollMsg does not touch the queue")
}

// --- Test 9: GitPollMsg sets header gitFiles ---

func TestApp_GitPollMsgRoutesToHeaderSetGitFiles(t *testing.T) {
	a := newTestApp(t, "sess-1", "", &stubWorkspace{})
	files := []workspace.ModifiedFile{{Path: "x.go"}, {Path: "y.go"}}

	a.Update(GitPollMsg{Files: files})
	assert.Equal(t, files, a.header.gitFiles)
}

// --- Test 10: MdWatchErrMsg captures err, no quit ---

func TestApp_MdWatchErrDoesNotQuitSetsWatchErr(t *testing.T) {
	a := newTestApp(t, "sess-1", "", &stubWorkspace{})

	_, cmd := a.Update(MdWatchErrMsg{Err: errors.New("simulated")})
	require.Nil(t, cmd, "watch error must not produce tea.Quit")
	require.NotNil(t, a.watchErr)
}

// --- Tests 11/12: Focus/Blur route to dispatcher ---

func TestApp_FocusBlurRoutesToNotifySetFocused(t *testing.T) {
	a := newTestApp(t, "sess-1", "", &stubWorkspace{})

	a.Update(tea.BlurMsg{})
	assert.False(t, a.notify.focused, "BlurMsg → notify.focused=false")

	a.Update(tea.FocusMsg{})
	assert.True(t, a.notify.focused, "FocusMsg → notify.focused=true")
}

// --- Test 13: Session pubsub updates header ---

func TestApp_PubsubSessionUpdatedCallsHeaderSetSession(t *testing.T) {
	a := newTestApp(t, "sess-1", "", &stubWorkspace{})

	updated := session.Session{ID: "sess-1", Title: "renamed", PromptTokens: 10}
	a.Update(pubsub.Event[session.Session]{Type: pubsub.UpdatedEvent, Payload: updated})

	require.NotNil(t, a.header.sess)
	assert.Equal(t, "renamed", a.header.sess.Title)
	assert.Equal(t, int64(10), a.header.sess.PromptTokens)
}

// --- Test 14: Message pubsub is a no-op ---

func TestApp_PubsubMessageEventIsNoop(t *testing.T) {
	a := newTestApp(t, "sess-1", "", &stubWorkspace{})

	// Snapshot a couple of fields that the chat-event branch could plausibly
	// poke; verify nothing changed and Update doesn't return a cmd.
	beforeMd := append([]byte(nil), a.md...)
	_, cmd := a.Update(pubsub.Event[message.Message]{
		Type:    pubsub.UpdatedEvent,
		Payload: message.Message{ID: "msg-1"},
	})
	require.Nil(t, cmd, "message events do not yield commands (audit M3)")
	assert.Equal(t, beforeMd, a.md, "message events do not mutate the .md buffer")
}

// --- Test 15: Notification pubsub routes to dispatcher ---

func TestApp_PubsubNotificationRoutesToDispatcher(t *testing.T) {
	a := newTestApp(t, "sess-1", "", &stubWorkspace{})
	a.notify = NewNotificationDispatcher(&config.Config{Options: &config.Options{}})
	rec := &recordingBackend{}
	a.notify.SetBackend(rec)
	a.notify.SetFocused(false)

	a.Update(pubsub.Event[notify.Notification]{
		Payload: notify.Notification{Type: notify.TypeAgentFinished, SessionTitle: "demo"},
	})
	calls := rec.calls()
	require.Len(t, calls, 1, "AgentFinished + unfocused → 1 backend send")
	assert.Contains(t, calls[0].Message, "demo")
}

// keyPress + testKeyMsg helpers used across the routing tests.
type testKeyMsg struct {
	text string
}

func (t testKeyMsg) Key() tea.Key   { return tea.Key{Text: t.text} }
func (t testKeyMsg) String() string { return t.text }

func keyPress(k string) tea.KeyMsg { return testKeyMsg{text: k} }

// Ensure mockWorkspace satisfies SandboxProvider. catwalk import keeps the
// model field's CatwalkCfg type live for tests that exercise ctx%.
var _ = catwalk.Model{}

// notification.Notification is referenced indirectly via the dispatcher; the
// import keeps the test surface explicit.
var _ notification.Backend = (*recordingBackend)(nil)
