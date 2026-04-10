package main

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// DB wraps *sql.DB with the driver name for placeholder-style conversion.
// Use ph() to rewrite ? placeholders to $N before passing queries to Exec/Query.
type DB struct {
	*sql.DB
	driver string
}

var rePlaceholder = regexp.MustCompile(`\?`)

// ph returns query with ? placeholders rewritten to $N for Postgres.
// For sqlite the query is returned unchanged.
func (d *DB) ph(query string) string {
	if d.driver != "postgres" {
		return query
	}
	n := 0
	return rePlaceholder.ReplaceAllStringFunc(query, func(string) string {
		n++
		return "$" + strconv.Itoa(n)
	})
}

const schema = `
CREATE TABLE IF NOT EXISTS workspaces (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
    workspace_id TEXT NOT NULL,
    key          TEXT NOT NULL,
    value        TEXT NOT NULL,
    PRIMARY KEY (workspace_id, key),
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id)
);

CREATE TABLE IF NOT EXISTS nodes (
    workspace_id TEXT NOT NULL,
    id           TEXT NOT NULL,
    label        TEXT NOT NULL,
    parent_id    TEXT NOT NULL DEFAULT '',
    amount       REAL NOT NULL DEFAULT 0,
    PRIMARY KEY (workspace_id, id),
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id)
);
`

// OpenDB opens (or creates) the database at dsn using the given driver.
// driver must be "sqlite" or "postgres". SQLite-specific pragmas are applied
// only when driver is "sqlite".
func OpenDB(driver, dsn string) (*DB, error) {
	sqlDB, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db := &DB{DB: sqlDB, driver: driver}
	if driver == "sqlite" {
		// Single writer prevents SQLITE_BUSY under concurrent Go goroutines.
		db.SetMaxOpenConns(1)
		if _, err := db.Exec(`
			PRAGMA journal_mode = WAL;
			PRAGMA synchronous  = NORMAL;
			PRAGMA foreign_keys = ON;
		`); err != nil {
			db.Close()
			return nil, fmt.Errorf("db pragmas: %w", err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("db schema: %w", err)
	}
	return db, nil
}

// LoadStore reads a workspace's data from the database and returns a fully
// hydrated Store. The income root node is guaranteed to be present.
func LoadStore(db *DB, workspaceID string) (*Store, error) {
	s := &Store{
		workspaceID: workspaceID,
		nodes:       make(map[string]Node),
		currency:    "£",
		db:          db,
	}

	rows, err := db.Query(db.ph(`SELECT key, value FROM settings WHERE workspace_id = ?`), workspaceID)
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		switch k {
		case "income":
			s.incomeAmount, _ = strconv.ParseFloat(v, 64)
		case "currency":
			s.currency = v
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings rows: %w", err)
	}

	rows2, err := db.Query(db.ph(`SELECT id, label, parent_id, amount FROM nodes WHERE workspace_id = ?`), workspaceID)
	if err != nil {
		return nil, fmt.Errorf("load nodes: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var n Node
		if err := rows2.Scan(&n.ID, &n.Label, &n.ParentID, &n.Amount); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		s.nodes[n.ID] = n
	}
	if err := rows2.Err(); err != nil {
		return nil, fmt.Errorf("nodes rows: %w", err)
	}

	// Guarantee the income root is always present.
	if _, ok := s.nodes["income"]; !ok {
		s.nodes["income"] = Node{ID: "income", Label: "Income"}
	}

	return s, nil
}

// dbListWorkspaces returns all workspaces ordered by name.
func dbListWorkspaces(db *DB) ([]Workspace, error) {
	rows, err := db.Query(`SELECT id, name FROM workspaces ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workspace
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.Name); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// dbCreateWorkspace inserts a new workspace row and seeds its income root node.
func dbCreateWorkspace(db *DB, id, name string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(db.ph(`INSERT INTO workspaces (id, name) VALUES (?, ?)`), id, name); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(
		db.ph(`INSERT INTO nodes (workspace_id, id, label, parent_id, amount) VALUES (?, 'income', 'Income', '', 0)`),
		id,
	); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// dbDeleteWorkspace removes a workspace and all its associated nodes and settings.
func dbDeleteWorkspace(db *DB, id string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, table := range []string{"nodes", "settings"} {
		if _, err := tx.Exec(db.ph(`DELETE FROM `+table+` WHERE workspace_id = ?`), id); err != nil {
			tx.Rollback()
			return err
		}
	}
	if _, err := tx.Exec(db.ph(`DELETE FROM workspaces WHERE id = ?`), id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// dbUpsertNode inserts or updates a node row in the given workspace.
func dbUpsertNode(db *DB, workspaceID string, n Node) error {
	_, err := db.Exec(db.ph(`
		INSERT INTO nodes (workspace_id, id, label, parent_id, amount) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id, id) DO UPDATE SET
			label     = excluded.label,
			parent_id = excluded.parent_id,
			amount    = excluded.amount`),
		workspaceID, n.ID, n.Label, n.ParentID, n.Amount,
	)
	return err
}

// dbDeleteNodes removes a set of node IDs in a single transaction for the given workspace.
func dbDeleteNodes(db *DB, workspaceID string, ids []string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(db.ph(`DELETE FROM nodes WHERE workspace_id = ? AND id = ?`))
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, id := range ids {
		if _, err := stmt.Exec(workspaceID, id); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// dbSaveSetting upserts a key/value setting for the given workspace.
func dbSaveSetting(db *DB, workspaceID, key, value string) error {
	_, err := db.Exec(db.ph(`
		INSERT INTO settings (workspace_id, key, value) VALUES (?, ?, ?)
		ON CONFLICT(workspace_id, key) DO UPDATE SET value = excluded.value`),
		workspaceID, key, value,
	)
	return err
}
