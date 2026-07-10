package main

import (
	"strings"
	"testing"
)

func TestRedactDBURL(t *testing.T) {
	// Postgres URL: password must be gone, username + host kept.
	got := redactDBURL("postgres://tcf:secretpass@host:5432/db?sslmode=disable")
	if strings.Contains(got, "secretpass") {
		t.Fatalf("password leaked in %q", got)
	}
	if !strings.Contains(got, "tcf@host:5432") {
		t.Fatalf("expected username+host preserved, got %q", got)
	}
	// SQLite URL: no credentials, returned unchanged.
	sqliteURL := "sqlite:///data/tcf.db"
	if redactDBURL(sqliteURL) != sqliteURL {
		t.Fatalf("sqlite url should be unchanged, got %q", redactDBURL(sqliteURL))
	}
}
