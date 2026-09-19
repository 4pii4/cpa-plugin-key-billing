package sqlite

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"cpa-key-billing/internal/billing"
)

func TestV14CodexRoutingMigrationPreservesHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v14.db")
	legacy, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	// V14 is the current schema without the new routing table.
	if _, err := legacy.Exec(strings.TrimPrefix(schema, codexRoutingSchema) + `
		INSERT INTO api_keys(scope,label) VALUES('dummy-scope','Keep me');
		INSERT INTO request_events(id,at,scope,failed) VALUES(1,1,'dummy-scope',1),(2,2,'dummy-scope',0);
		INSERT INTO request_errors(request_event_id,status_code) VALUES(1,429);
		PRAGMA user_version=14;`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var events, errors int
	if err := db.db.QueryRow("SELECT count(*) FROM request_events").Scan(&events); err != nil || events != 2 {
		t.Fatal(events, err)
	}
	if err := db.db.QueryRow("SELECT count(*) FROM request_errors").Scan(&errors); err != nil || errors != 1 {
		t.Fatal(errors, err)
	}
	state := mustLoad(t, db).State
	if state.CodexRouting.Enabled || state.Keys["dummy-scope"].Label != "Keep me" {
		t.Fatal("migration changed existing configuration")
	}
	ref := billing.CredentialFingerprint("dummy-pro-account")
	state.CodexRouting = billing.CodexRouting{Enabled: true, Roles: map[string]string{ref: "reserve"}}
	mustSave(t, db, state, billing.Changes{CodexRouting: true, ConfigCredentials: true})
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openDatabase(t, path)
	loaded := mustLoad(t, db).State.CodexRouting
	if !loaded.Enabled || loaded.Roles[ref] != "reserve" {
		t.Fatal("routing settings were not persisted", loaded)
	}
}

func TestV14CodexMigrationRollsBackOnConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conflict.db")
	legacy, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	if _, err := legacy.Exec(strings.TrimPrefix(schema, codexRoutingSchema) + `CREATE TABLE codex_routing(marker TEXT); INSERT INTO codex_routing VALUES('keep'); PRAGMA user_version=14;`); err != nil {
		t.Fatal(err)
	}
	if db, err := Open(path); err == nil {
		db.Close()
		t.Fatal("conflicting schema accepted")
	}
	var version int
	if err := legacy.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 14 {
		t.Fatal("migration was not atomic", version, err)
	}
	var marker string
	if err := legacy.QueryRow("SELECT marker FROM codex_routing").Scan(&marker); err != nil || marker != "keep" {
		t.Fatal("existing data changed", marker, err)
	}
}
