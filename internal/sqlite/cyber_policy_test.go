package sqlite

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-key-billing/internal/billing"
)

func TestV15CyberPolicyMigrationAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v15.db")
	legacy, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	legacySchema := strings.Replace(schema, cyberPolicySchema, "", 1)
	if _, err := legacy.Exec(legacySchema + `
		INSERT INTO api_keys(scope,label) VALUES('dummy-scope','Keep me');
		INSERT INTO request_events(id,at,scope,failed) VALUES(1,1,'dummy-scope',0);
		PRAGMA user_version=15;`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	state := mustLoad(t, db).State
	if state.Keys["dummy-scope"].Label != "Keep me" {
		t.Fatal("migration changed existing key state")
	}
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	state.CyberPolicy = billing.CyberPolicy{
		Settings: billing.CyberPolicySettings{Enabled: true, BaseDelaySeconds: 45},
		Bans: map[string]billing.CyberPolicyBan{
			"dummy-scope": {Consecutive: 3, BlockedUntil: now.Add(time.Hour), LastDetectedAt: now, LastWasCyber: true},
		},
	}
	mustSave(t, db, state, billing.Changes{CyberPolicy: true})
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openDatabase(t, path)
	loaded := mustLoad(t, db).State.CyberPolicy
	if !loaded.Settings.Enabled || loaded.Settings.BaseDelaySeconds != 45 ||
		loaded.Bans["dummy-scope"].Consecutive != 3 || !loaded.Bans["dummy-scope"].BlockedUntil.Equal(now.Add(time.Hour)) {
		t.Fatalf("cyber-policy state was not persisted: %+v", loaded)
	}
}

func TestV15CyberPolicyMigrationRollsBackOnConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conflict.db")
	legacy, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	legacySchema := strings.Replace(schema, cyberPolicySchema, "", 1)
	if _, err := legacy.Exec(legacySchema + `CREATE TABLE cyber_policy(marker TEXT); INSERT INTO cyber_policy VALUES('keep'); PRAGMA user_version=15;`); err != nil {
		t.Fatal(err)
	}
	if db, err := Open(path); err == nil {
		db.Close()
		t.Fatal("conflicting schema accepted")
	}
	var version int
	if err := legacy.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 15 {
		t.Fatal("migration was not atomic", version, err)
	}
	var marker string
	if err := legacy.QueryRow("SELECT marker FROM cyber_policy").Scan(&marker); err != nil || marker != "keep" {
		t.Fatal("existing data changed", marker, err)
	}
}
