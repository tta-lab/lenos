package agent

import (
	"context"
	"log/slog"
	"time"

	"charm.land/fantasy"

	"github.com/tta-lab/lenos/internal/message"
)

var modelStreamRetryOptions = fantasy.DefaultRetryOptions

func retryModelStream[T any](
	ctx context.Context,
	operation func() (T, error),
	onRetry func(),
) (T, error) {
	opts := modelStreamRetryOptions()
	opts.OnRetry = func(err *fantasy.ProviderError, delay time.Duration) {
		slog.Warn("model stream retry", "error", err, "delay", delay)
		if onRetry != nil {
			onRetry()
		}
	}
	retry := fantasy.RetryWithExponentialBackoffRespectingRetryHeaders[T](opts)
	return retry(ctx, operation)
}

func resetMessageForStreamRetry(
	ctx context.Context,
	messages message.Service,
	msg *message.Message,
	baseline message.Message,
	logMessage string,
) {
	*msg = baseline.Clone()
	if err := messages.Update(ctx, *msg); err != nil {
		slog.Warn(logMessage, "error", err)
	}
}
