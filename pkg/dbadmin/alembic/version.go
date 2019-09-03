package alembic

import (
	"github.com/app-sre/dba-operator/pkg/dbadmin"
)

// MigrationEngine is a type which implements the MigrationEngine
// interface for Alembic migrations
type MigrationEngine struct{}

// CreateMigrationEngine instantiates an MigrationEngine
func CreateMigrationEngine() dbadmin.MigrationEngine {
	return &MigrationEngine{}
}

// GetVersionQuery implements MigrationEngine
func (amm *MigrationEngine) GetVersionQuery() string {
	return "SELECT version_num FROM alembic_version LIMIT 1"
}
