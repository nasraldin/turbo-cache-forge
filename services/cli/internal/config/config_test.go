package config

import (
	"os"
	"runtime"
	"testing"
)

func TestLoadMissingFileReturnsZeroValue(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if f != (File{}) {
		t.Fatalf("Load() on missing file = %+v, want zero value", f)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	want := File{APIURL: "https://cache.example.com", Token: "eyJ.secret.jwt"}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

func TestSavePermissionsAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits don't apply on windows")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(File{Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	p, _ := Path()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("config file perm = %o, want 0600", perm)
	}
}

func TestPickPrecedence(t *testing.T) {
	cases := []struct{ name, flag, env, file, want string }{
		{"flag wins", "f", "e", "c", "f"},
		{"env wins over file", "", "e", "c", "e"},
		{"file is the fallback", "", "", "c", "c"},
		{"all empty", "", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Pick(tc.flag, tc.env, tc.file); got != tc.want {
				t.Errorf("Pick(%q,%q,%q) = %q, want %q", tc.flag, tc.env, tc.file, got, tc.want)
			}
		})
	}
}
