package obs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

// fakeTransport captures events instead of sending them over the network.
// This installed sentry-go version's Transport interface requires
// Flush/FlushWithContext/Configure/SendEvent/Close (confirmed via
// `go doc github.com/getsentry/sentry-go.Transport` against the resolved
// v0.47.0 — newer than the brief's long-stable baseline of
// Flush/Configure/SendEvent, so FlushWithContext and Close are added here).
type fakeTransport struct{ events []*sentry.Event }

func (f *fakeTransport) Configure(sentry.ClientOptions)        {}
func (f *fakeTransport) SendEvent(e *sentry.Event)             { f.events = append(f.events, e) }
func (f *fakeTransport) Flush(time.Duration) bool              { return true }
func (f *fakeTransport) FlushWithContext(context.Context) bool { return true }
func (f *fakeTransport) Close()                                {}

func TestCaptureErrorNoopWithoutInit(t *testing.T) {
	// No sentry.Init anywhere in this test — CaptureError must not panic
	// and must not require a DSN to be safe to call.
	CaptureError(errors.New("boom"))
}

func TestCaptureErrorSendsEventWhenInitialized(t *testing.T) {
	ft := &fakeTransport{}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:       "https://public@example.com/1",
		Transport: ft,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sentry.CurrentHub().BindClient(nil) })

	CaptureError(errors.New("db unavailable"))
	sentry.Flush(time.Second)

	if len(ft.events) != 1 {
		t.Fatalf("got %d events, want 1", len(ft.events))
	}
}
