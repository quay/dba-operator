package alembic

import (
	"github.com/app-sre/dba-operator/pkg/dbadmin"
)

type AlembicMigrationMetadata struct {
}

func CreateAlembicMigrationMetadata() dbadmin.MigrationMetadata {
	return &AlembicMigrationMetadata{}
}

func (amm *AlembicMigrationMetadata) GetVersionQuery() string {
	return "SELECT version_num from alembic_version limit 1"
}
