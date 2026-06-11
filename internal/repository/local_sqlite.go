package repository

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const localDataSQLiteFile = "rule-source-indexes.sqlite"

type sqliteSchema struct {
	Name       string
	Statements []string
}

func openLocalSQLite(dir string, schemas ...sqliteSchema) (*sql.DB, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dir, localDataSQLiteFile)
	db, err := openSQLiteDB(dbPath)
	if err != nil {
		return nil, err
	}
	if err := applySQLiteSchemas(db, schemas...); err != nil {
		schemaErr := err
		_ = db.Close()
		if !looksLikeSQLiteCorruption(schemaErr) {
			return nil, schemaErr
		}
		if resetErr := resetSQLiteFiles(dbPath); resetErr != nil {
			return nil, fmt.Errorf("reset corrupted SQLite database: %w", resetErr)
		}
		db, err = openSQLiteDB(dbPath)
		if err != nil {
			return nil, err
		}
		if applyErr := applySQLiteSchemas(db, schemas...); applyErr != nil {
			_ = db.Close()
			return nil, applyErr
		}
		if eventErr := recordLocalSQLiteDiagnostic(
			db,
			"sqlite_database_reinitialized",
			fmt.Sprintf("Local SQLite database was reinitialized after corruption was detected: %v", schemaErr),
		); eventErr != nil {
			_ = db.Close()
			return nil, eventErr
		}
	}
	return db, nil
}

func openSQLiteDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func applySQLiteSchemas(db *sql.DB, schemas ...sqliteSchema) error {
	baseStatements := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS local_data_diagnostic_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type TEXT NOT NULL,
			message TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
	}
	for _, statement := range baseStatements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	for _, schema := range schemas {
		for _, statement := range schema.Statements {
			if _, err := db.Exec(statement); err != nil {
				return fmt.Errorf("apply SQLite schema %s: %w", schema.Name, err)
			}
		}
	}
	return nil
}

func resetSQLiteFiles(dbPath string) error {
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func looksLikeSQLiteCorruption(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database disk image is malformed") ||
		strings.Contains(message, "file is not a database") ||
		strings.Contains(message, "database is locked") && strings.Contains(message, "malformed")
}

func recordLocalSQLiteDiagnostic(db *sql.DB, eventType string, message string) error {
	_, err := db.Exec(
		`INSERT INTO local_data_diagnostic_events (event_type, message, created_at) VALUES (?, ?, ?)`,
		eventType,
		message,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func queryLimit(value int, defaultLimit int, maxLimit int) int {
	if defaultLimit <= 0 {
		defaultLimit = 50
	}
	if maxLimit <= 0 {
		maxLimit = defaultLimit
	}
	if value <= 0 {
		value = defaultLimit
	}
	if value > maxLimit {
		return maxLimit
	}
	return value
}

func queryOffset(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
