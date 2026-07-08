package config

import (
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", c.Addr)
	}
	if c.StorageBackend != "fs" {
		t.Errorf("StorageBackend = %q, want fs", c.StorageBackend)
	}
	if c.MaxUploadBytes != 1<<30 {
		t.Errorf("MaxUploadBytes = %d, want %d", c.MaxUploadBytes, 1<<30)
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when DATABASE_URL is unset")
	}
}
