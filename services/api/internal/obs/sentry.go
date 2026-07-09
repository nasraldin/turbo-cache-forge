package obs

import (
	"os"
	"time"

	"github.com/getsentry/sentry-go"
)

// InitSentry configures the Sentry SDK when SENTRY_DSN is set; otherwise it
// does nothing. sentry-go's package-level CaptureException is a safe no-op
// when Init was never called — there is no separate "enabled" flag to check
// — so CaptureError below is unconditionally safe to call from every error
// path, gated or not.
func InitSentry() (flush func(), err error) {
	dsn := os.Getenv("SENTRY_DSN")
	if dsn == "" {
		return func() {}, nil
	}
	if err := sentry.Init(sentry.ClientOptions{Dsn: dsn}); err != nil {
		return nil, err
	}
	return func() { sentry.Flush(2 * time.Second) }, nil
}

// CaptureError reports err to Sentry (a no-op if InitSentry was never called
// with a DSN). Call it only where a storage/DB error becomes a 5xx response
// — that's the "storage/DB errors" surface Phase 2 scopes this to. Client
// mistakes (bad hash, oversized upload, bad auth) are 4xx and must never be
// reported here.
func CaptureError(err error) {
	if err == nil {
		return
	}
	sentry.CaptureException(err)
}
