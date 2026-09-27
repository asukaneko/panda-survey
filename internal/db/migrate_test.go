package db

import (
	"path/filepath"
	"testing"
)

func TestV4BackfillOnLegacyDB(t *testing.T) {
	p := filepath.Join(t.TempDir(), "legacy.db")
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO users(username, password_hash, created_at) VALUES ('u','h','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO surveys(user_id, title, description, status, created_at, updated_at) VALUES (1,'t','',0,'now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DROP INDEX idx_surveys_public_token`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`ALTER TABLE surveys DROP COLUMN public_token`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE schema_version SET version = 3`); err != nil {
		t.Fatal(err)
	}
	d.Close()

	d2, err := Open(p)
	if err != nil {
		t.Fatalf("legacy upgrade failed: %v", err)
	}
	defer d2.Close()
	var token string
	if err := d2.QueryRow(`SELECT public_token FROM surveys WHERE id = 1`).Scan(&token); err != nil {
		t.Fatal(err)
	}
	if len(token) != 32 {
		t.Fatalf("backfilled token = %q", token)
	}
	var ver int
	if err := d2.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 4 {
		t.Fatalf("schema version = %d", ver)
	}
}
