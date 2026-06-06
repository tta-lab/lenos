package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"

	"charm.land/fantasy"
	"charm.land/lipgloss/v2"

	"github.com/tta-lab/lenos/internal/agent/hyper"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/stringext"
)

// errorFinishFor returns an appropriate FinishReason and user-facing message
// for a run error. This provides actionable feedback (e.g. "enable Copilot
// model", "add credits") rather than opaque error strings.
func errorFinishFor(runErr error, model string) (reason message.FinishReason, title, msg string) {
	reason = message.FinishReasonError
	const defaultTitle = "Provider Error"
	linkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6b8a5e")).Underline(true)

	if errors.Is(runErr, hyper.ErrNoCredits) {
		url := hyper.BaseURL()
		link := linkStyle.Hyperlink(url, "id=hyper").Render(url)
		return reason, "No credits", "You're out of credits. Add more at " + link
	}

	var fantasyErr *fantasy.Error
	var providerErr *fantasy.ProviderError
	if errors.As(runErr, &providerErr) {
		if providerErr.Message == "The requested model is not supported." {
			url := "https://github.com/settings/copilot/features"
			link := linkStyle.Hyperlink(url, "id=copilot").Render(url)
			return reason, "Copilot model not enabled",
				fmt.Sprintf("%q is not enabled in Copilot. Go to the following page to enable it. Then, wait 5 minutes before trying again. %s", model, link)
		}
		return reason, cmp.Or(stringext.Capitalize(providerErr.Title), defaultTitle), providerErr.Message
	}
	if errors.As(runErr, &fantasyErr) {
		return reason, cmp.Or(stringext.Capitalize(fantasyErr.Title), defaultTitle), fantasyErr.Message
	}
	return reason, defaultTitle, runErr.Error()
}

// attachErrorFinish updates the most-recent assistant message in the session
// with a user-facing FinishReasonError + title + detail derived from the
// loop's run error. The loop creates assistant rows as it streams; this
// follow-up replaces any tool-use/end-turn finish on the LAST one with an
// error-flavored finish so the UI banner makes sense.
//
// Boundary note: attach the error banner to the newest durable assistant row
// rather than assuming every streamed emit still exists.
func (a *sessionAgent) attachErrorFinish(ctx context.Context, sessionID string, runErr error, model string) {
	all, listErr := a.messages.List(ctx, sessionID)
	if listErr != nil {
		slog.Warn("attachErrorFinish: list messages", "error", listErr)
		return
	}
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Role != message.Assistant {
			continue
		}
		latest := all[i]
		_, title, detail := errorFinishFor(runErr, model)
		latest.AddFinish(message.FinishReasonError, title, detail)
		if updateErr := a.messages.Update(ctx, latest); updateErr != nil {
			slog.Warn("attachErrorFinish: update", "error", updateErr)
		}
		return
	}
}
