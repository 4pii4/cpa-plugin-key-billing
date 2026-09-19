package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"cpa-key-billing/internal/billing"
)

func (d *DB) loadCodexRouting(state *billing.State) error {
	var raw string
	err := d.db.QueryRow("SELECT settings_json FROM codex_routing WHERE id=1").Scan(&raw)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(raw), &state.CodexRouting); err != nil {
		return fmt.Errorf("read Codex routing settings: %w", err)
	}
	return nil
}

func saveCodexRouting(tx *sql.Tx, state *billing.State) error {
	raw, err := json.Marshal(state.CodexRouting)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO codex_routing(id,settings_json) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET settings_json=excluded.settings_json", string(raw))
	return err
}
