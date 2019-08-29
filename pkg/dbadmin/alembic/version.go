package alembic

import (
	"github.com/app-sre/dba-operator/pkg/dbadmin"
)

type AlembicMigrationEngine struct{}

func CreateAlembicMigrationEngine() dbadmin.MigrationEngine {
	return &AlembicMigrationEngine{}
}

func (amm *AlembicMigrationEngine) GetVersionQuery() string {
	return "SELECT version_num FROM alembic_version LIMIT 1"
}
