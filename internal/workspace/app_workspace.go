package workspace

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"sort"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/tta-lab/lenos/internal/agent"
	"github.com/tta-lab/lenos/internal/app"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/oauth"
	"golang.org/x/sync/errgroup"

	"github.com/tta-lab/lenos/internal/session"
)

// AppWorkspace implements the Workspace interface by delegating
// directly to an in-process [app.App] instance. This is the default
// mode when the client/server architecture is not enabled.
type AppWorkspace struct {
	app   *app.App
	store *config.ConfigStore
}

// NewAppWorkspace creates a new AppWorkspace wrapping the given app
// and config store.
func NewAppWorkspace(a *app.App, store *config.ConfigStore) *AppWorkspace {
	return &AppWorkspace{
		app:   a,
		store: store,
	}
}

// -- Sessions --

func (w *AppWorkspace) CreateSession(ctx context.Context, title string) (session.Session, error) {
	return w.app.Sessions.Create(ctx, title)
}

func (w *AppWorkspace) GetSession(ctx context.Context, sessionID string) (session.Session, error) {
	return w.app.Sessions.Get(ctx, sessionID)
}

func (w *AppWorkspace) ListSessions(ctx context.Context) ([]session.Session, error) {
	return w.app.Sessions.List(ctx)
}

func (w *AppWorkspace) SaveSession(ctx context.Context, sess session.Session) (session.Session, error) {
	return w.app.Sessions.Save(ctx, sess)
}

func (w *AppWorkspace) DeleteSession(ctx context.Context, sessionID string) error {
	return w.app.Sessions.Delete(ctx, sessionID)
}

func (w *AppWorkspace) CreateAgentToolSessionID(messageID, toolCallID string) string {
	return w.app.Sessions.CreateAgentToolSessionID(messageID, toolCallID)
}

func (w *AppWorkspace) ParseAgentToolSessionID(sessionID string) (string, string, bool) {
	return w.app.Sessions.ParseAgentToolSessionID(sessionID)
}

// -- Messages --

func (w *AppWorkspace) ListMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	return w.app.Messages.List(ctx, sessionID)
}

func (w *AppWorkspace) ListUserMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	return w.app.Messages.ListUserMessages(ctx, sessionID)
}

func (w *AppWorkspace) ListAllUserMessages(ctx context.Context) ([]message.Message, error) {
	return w.app.Messages.ListAllUserMessages(ctx)
}

// -- Agent --

func (w *AppWorkspace) AgentRun(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) error {
	if w.app.AgentCoordinator == nil {
		return errors.New("agent coordinator not initialized")
	}
	_, err := w.app.AgentCoordinator.Run(ctx, sessionID, prompt, attachments...)
	return err
}

func (w *AppWorkspace) AgentCancel(sessionID string) {
	if w.app.AgentCoordinator != nil {
		w.app.AgentCoordinator.Cancel(sessionID)
	}
}

func (w *AppWorkspace) AgentIsBusy() bool {
	if w.app.AgentCoordinator == nil {
		return false
	}
	return w.app.AgentCoordinator.IsBusy()
}

func (w *AppWorkspace) AgentIsSessionBusy(sessionID string) bool {
	if w.app.AgentCoordinator == nil {
		return false
	}
	return w.app.AgentCoordinator.IsSessionBusy(sessionID)
}

func (w *AppWorkspace) AgentModel() AgentModel {
	if w.app.AgentCoordinator == nil {
		return AgentModel{}
	}
	m := w.app.AgentCoordinator.Model()
	return AgentModel{
		CatwalkCfg: m.CatwalkCfg,
		ModelCfg:   m.ModelCfg,
	}
}

func (w *AppWorkspace) AgentIsReady() bool {
	return w.app.AgentCoordinator != nil
}

func (w *AppWorkspace) AgentQueuedPrompts(sessionID string) int {
	if w.app.AgentCoordinator == nil {
		return 0
	}
	return w.app.AgentCoordinator.QueuedPrompts(sessionID)
}

func (w *AppWorkspace) AgentQueuedPromptsList(sessionID string) []string {
	if w.app.AgentCoordinator == nil {
		return nil
	}
	return w.app.AgentCoordinator.QueuedPromptsList(sessionID)
}

func (w *AppWorkspace) AgentClearQueue(sessionID string) {
	if w.app.AgentCoordinator != nil {
		w.app.AgentCoordinator.ClearQueue(sessionID)
	}
}

func (w *AppWorkspace) AgentSummarize(ctx context.Context, sessionID string) error {
	if w.app.AgentCoordinator == nil {
		return errors.New("agent coordinator not initialized")
	}
	return w.app.AgentCoordinator.Summarize(ctx, sessionID)
}

func (w *AppWorkspace) UpdateAgentModel(ctx context.Context) error {
	return w.app.UpdateAgentModel(ctx)
}

func (w *AppWorkspace) InitCoderAgent(ctx context.Context) error {
	return w.app.InitCoderAgent(ctx)
}

// -- Config (read-only) --

func (w *AppWorkspace) Config() *config.Config {
	return w.store.Config()
}

func (w *AppWorkspace) WorkingDir() string {
	return w.store.WorkingDir()
}

func (w *AppWorkspace) Resolver() config.VariableResolver {
	return w.store.Resolver()
}

// -- Config mutations --

func (w *AppWorkspace) UpdatePreferredModel(scope config.Scope, model config.SelectedModel) error {
	return w.store.UpdatePreferredModel(scope, model)
}

func (w *AppWorkspace) SetCompactMode(scope config.Scope, enabled bool) error {
	return w.store.SetCompactMode(scope, enabled)
}

func (w *AppWorkspace) SetProviderAPIKey(scope config.Scope, providerID string, apiKey any) error {
	return w.store.SetProviderAPIKey(scope, providerID, apiKey)
}

func (w *AppWorkspace) SetConfigField(scope config.Scope, key string, value any) error {
	return w.store.SetConfigField(scope, key, value)
}

func (w *AppWorkspace) RemoveConfigField(scope config.Scope, key string) error {
	return w.store.RemoveConfigField(scope, key)
}

func (w *AppWorkspace) ImportCopilot() (*oauth.Token, bool) {
	return w.store.ImportCopilot()
}

func (w *AppWorkspace) RefreshOAuthToken(ctx context.Context, scope config.Scope, providerID string) error {
	return w.store.RefreshOAuthToken(ctx, scope, providerID)
}

// -- Project lifecycle --

func (w *AppWorkspace) ProjectNeedsInitialization() (bool, error) {
	return config.ProjectNeedsInitialization(w.store)
}

func (w *AppWorkspace) MarkProjectInitialized() error {
	return config.MarkProjectInitialized(w.store)
}

func (w *AppWorkspace) InitializePrompt() (string, error) {
	return agent.InitializePrompt(w.store)
}

// -- Lifecycle --

func (w *AppWorkspace) Subscribe(program *tea.Program) {
	w.app.Subscribe(program)
}

func (w *AppWorkspace) Shutdown() {
	w.app.Shutdown()
}

// App returns the underlying app.App instance.
func (w *AppWorkspace) App() *app.App {
	return w.app
}

// Store returns the underlying config store.
func (w *AppWorkspace) Store() *config.ConfigStore {
	return w.store
}

func (w *AppWorkspace) AgentName() string {
	return w.store.Overrides().AgentName
}

// -- Git --

func (w *AppWorkspace) IsGitWorktree(ctx context.Context) bool {
	err := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree").Run()
	return err == nil
}

func (w *AppWorkspace) ListModifiedFiles(ctx context.Context) ([]ModifiedFile, error) {
	dir := w.store.WorkingDir()
	var tracked, untracked []ModifiedFile
	var g errgroup.Group

	g.Go(func() error {
		var err error
		tracked, err = trackedNumstat(ctx, dir)
		return err
	})
	g.Go(func() error {
		var err error
		untracked, err = untrackedFiles(ctx, dir)
		return err
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	files := append(tracked, untracked...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// trackedNumstat runs git diff HEAD --numstat and returns ModifiedFiles for
// all tracked files that differ from HEAD (staged + unstaged combined).
func trackedNumstat(ctx context.Context, dir string) ([]ModifiedFile, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "diff", "HEAD", "--numstat", "--no-renames")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var files []ModifiedFile
	for _, line := range bytes.Split(bytes.TrimSpace(out), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		fields := bytes.SplitN(line, []byte{'\t'}, 3)
		if len(fields) != 3 {
			continue
		}
		isBinary := bytes.Equal(fields[0], []byte("-"))
		added, _ := strconv.Atoi(string(fields[0]))
		deleted, _ := strconv.Atoi(string(fields[1]))
		files = append(files, ModifiedFile{
			Path:     string(fields[2]),
			Added:    added,
			Deleted:  deleted,
			IsBinary: isBinary,
		})
	}
	return files, nil
}

// untrackedFiles returns ModifiedFiles for all untracked files, respecting
// .gitignore and standard excludes.
func untrackedFiles(ctx context.Context, dir string) ([]ModifiedFile, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "ls-files", "--others", "--exclude-standard")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var files []ModifiedFile
	for _, line := range bytes.Split(bytes.TrimSpace(out), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		files = append(files, ModifiedFile{
			Path:  string(line),
			Added: -1,
			IsNew: true,
		})
	}
	return files, nil
}

// Compile-time check that AppWorkspace implements Workspace.
var _ Workspace = (*AppWorkspace)(nil)
