package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"cpa-key-billing/internal/billing"
)

func replaceRoutes(tx *sql.Tx, state *billing.State) error {
	if _, err := tx.Exec("DELETE FROM routes"); err != nil {
		return fmt.Errorf("clear routing rules: %w", err)
	}
	for position, route := range state.Routes {
		raw, err := json.Marshal(route.Rule)
		if err != nil {
			return fmt.Errorf("save routing rule %s: %w", route.ID, err)
		}
		if _, err = tx.Exec("INSERT INTO routes (position, id, name, rule_json) VALUES (?, ?, ?, ?)", position, route.ID, route.Name, string(raw)); err != nil {
			return fmt.Errorf("save routing rule %s: %w", route.ID, err)
		}
	}
	return nil
}

func (d *DB) loadRoutes(state *billing.State) error {
	rows, err := d.db.Query("SELECT id, name, rule_json FROM routes ORDER BY position")
	if err != nil {
		return fmt.Errorf("read routing rules: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var route billing.Route
		var raw string
		if err := rows.Scan(&route.ID, &route.Name, &raw); err != nil {
			return fmt.Errorf("read routing rules: %w", err)
		}
		if err := json.Unmarshal([]byte(raw), &route.Rule); err != nil {
			return fmt.Errorf("decode routing rule %s: %w", route.ID, err)
		}
		route, err = billing.NormalizeRoute(route)
		if err != nil {
			return fmt.Errorf("validate routing rule %s: %w", route.ID, err)
		}
		state.Routes = append(state.Routes, route)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read routing rules: %w", err)
	}
	return nil
}
