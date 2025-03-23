package db

import (
	"fmt"
	"os"
	"path/filepath"
)

func (d *Database) InitDatabase() error {
	schemaFile := filepath.Join("internal", "db", "schema.sql")

	schemaSQL, err := os.ReadFile(schemaFile)
	if err != nil {
		return fmt.Errorf("error reading schema file: %w", err)
	}

	_, err = d.db.Exec(string(schemaSQL))
	if err != nil {
		return fmt.Errorf("error executing schema: %w", err)
	}

	return nil
}
