package oidcdevice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginHappyPathAfterOnePendingPoll(t *testing.T) {
	var srv *httptest.Server
	tokenCalls := 0

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Discovery{
			DeviceAuthorizationEndpoint: srv.URL + "/device_authorization",
			TokenEndpoint:               srv.URL + "/token",
		})
	})
	mux.HandleFunc("/device_authorization", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(DeviceAuthResponse{
			DeviceCode: "dc1", UserCode: "ABCD-EFGH",
			VerificationURI: srv.URL + "/verify", ExpiresIn: 600, Interval: 0,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		if tokenCalls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "eyJ.jwt.token"})
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	var prompted DeviceAuthResponse
	noSleep := func(time.Duration) {} // instant — no real waiting in tests

	token, err := Login(context.Background(), srv.URL, "cli-client", "openid",
		func(da DeviceAuthResponse) { prompted = da }, noSleep)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if token != "eyJ.jwt.token" {
		t.Fatalf("Login() = %q", token)
	}
	if prompted.UserCode != "ABCD-EFGH" {
		t.Fatalf("prompt did not receive the device auth response: %+v", prompted)
	}
	if tokenCalls != 2 {
		t.Fatalf("expected 2 poll calls (1 pending + 1 success), got %d", tokenCalls)
	}
}

func TestPollTokenExpiresWhenDeadlinePasses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
	}))
	defer srv.Close()

	disc := Discovery{TokenEndpoint: srv.URL}
	_, err := PollToken(context.Background(), disc, "cli-client", "dc1",
		0, time.Now().Add(-time.Second), // deadline already in the past
		func(time.Duration) {})
	if err != ErrExpired {
		t.Fatalf("PollToken() error = %v, want ErrExpired", err)
	}
}

func TestPollTokenAccessDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "access_denied"})
	}))
	defer srv.Close()

	disc := Discovery{TokenEndpoint: srv.URL}
	_, err := PollToken(context.Background(), disc, "cli-client", "dc1",
		0, time.Now().Add(time.Minute), func(time.Duration) {})
	if err != ErrAccessDenied {
		t.Fatalf("PollToken() error = %v, want ErrAccessDenied", err)
	}
}
