package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"cpa-key-billing/internal/billing"
)

func (d *DB) loadCyberPolicy(state *billing.State) error {
	var raw string
	err := d.db.QueryRow("SELECT state_json FROM cyber_policy WHERE id=1").Scan(&raw)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(raw), &state.CyberPolicy); err != nil {
		return fmt.Errorf("read cyber-policy cooldown state: %w", err)
	}
	return nil
}

func saveCyberPolicy(tx *sql.Tx, state *billing.State) error {
	raw, err := json.Marshal(state.CyberPolicy)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO cyber_policy(id,state_json) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET state_json=excluded.state_json", string(raw))
	return err
}
