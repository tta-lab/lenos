package agent

func (a *sessionAgent) getOrCreateBackgroundRunner(call SessionAgentCall) *BackgroundRunner {
	a.bgRunnersMu.Lock()
	defer a.bgRunnersMu.Unlock()
	if br, ok := a.bgRunners.Get(call.SessionID); ok && br != nil {
		return br
	}
	br := NewBackgroundRunner(a.enqueueBackgroundJobResult(call))
	setOnIdle(br, func() {
		a.bgRunnersMu.Lock()
		a.bgRunners.Del(call.SessionID)
		a.bgRunnersMu.Unlock()
	})
	a.bgRunners.Set(call.SessionID, br)
	return br
}

// setOnIdle writes the onIdle callback under onIdleMu to prevent races
// with the background goroutine in Track that reads it.
func setOnIdle(br *BackgroundRunner, f func()) {
	br.onIdleMu.Lock()
	br.onIdle = f
	br.onIdleMu.Unlock()
}

func (a *sessionAgent) cleanupBackgroundRunner(sessionID string, br *BackgroundRunner) {
	if br.ActiveCount() > 0 {
		a.bgRunners.Set(sessionID, br)
		return
	}
	a.bgRunnersMu.Lock()
	if current, ok := a.bgRunners.Get(sessionID); ok && current == br {
		a.bgRunners.Del(sessionID)
	}
	a.bgRunnersMu.Unlock()
}
