package db

import "testing"

func TestRebindPostgres(t *testing.T) {
	d := dialectFor("pgx")
	got := d.rebind("SELECT * FROM t WHERE a=? AND b=? AND c=?")
	want := "SELECT * FROM t WHERE a=$1 AND b=$2 AND c=$3"
	if got != want {
		t.Fatalf("rebind pg = %q, want %q", got, want)
	}
	if d.rebind("x=?::date") != "x=$1::date" {
		t.Fatalf("rebind must keep ::date suffix")
	}
}

func TestRebindSQLiteIsNoop(t *testing.T) {
	d := dialectFor("sqlite")
	q := "SELECT * FROM t WHERE a=? AND b=?"
	if d.rebind(q) != q {
		t.Fatalf("sqlite rebind must be a no-op")
	}
	if d.now != "CURRENT_TIMESTAMP" {
		t.Fatalf("sqlite now = %q, want CURRENT_TIMESTAMP", d.now)
	}
}

func TestParseURL(t *testing.T) {
	cases := []struct {
		in, driver string
	}{
		{"postgres://u:p@h:5432/db?sslmode=disable", "pgx"},
		{"postgresql://u@h/db", "pgx"},
		{"sqlite:///data/tcf.db", "sqlite"},
		{"file:/tmp/x.db", "sqlite"},
		{"/data/tcf.db", "sqlite"},
	}
	for _, c := range cases {
		drv, dsn, err := parseURL(c.in)
		if err != nil {
			t.Fatalf("parseURL(%q) err: %v", c.in, err)
		}
		if drv != c.driver {
			t.Fatalf("parseURL(%q) driver = %q, want %q", c.in, drv, c.driver)
		}
		if drv == "sqlite" && dsn == "" {
			t.Fatalf("parseURL(%q) produced empty sqlite dsn", c.in)
		}
	}
}
