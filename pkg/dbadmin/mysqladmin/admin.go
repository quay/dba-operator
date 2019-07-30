package mysqladmin

import (
	"database/sql"
	"fmt"

	"github.com/go-sql-driver/mysql"

	"github.com/app-sre/dba-operator/pkg/dbadmin"
)

type MySQLDbAdmin struct {
	handle   *sql.DB
	metadata dbadmin.MigrationMetadata
}

func CreateMySQLAdmin(username, password, hostname string, port uint16, database string, metadata dbadmin.MigrationMetadata) (dbadmin.DbAdmin, error) {
	connectionString := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", username, password, hostname, port, database)
	db, err := sql.Open("mysql", connectionString)

	return &MySQLDbAdmin{db, metadata}, err
}

func (mdba *MySQLDbAdmin) WriteCredentials(username, password string) error {
	return nil
}

func (mdba *MySQLDbAdmin) VerifyUnusedAndDeleteCredentials(username string) error {
	return nil
}

func (mdba *MySQLDbAdmin) GetSchemaVersion() (version string, err error) {
	var rows *sql.Rows
	rows, err = mdba.handle.Query(mdba.metadata.GetVersionQuery())
	if err != nil {
		mysqlErr, ok := err.(*mysql.MySQLError)
		if ok && mysqlErr.Number == 1146 {
			// No migration metadata, likely an empty database
			return "", nil
		}
		return version, err
	}

	defer rows.Close()

	err = rows.Scan(&version)
	if err != nil {
		return version, err
	}

	if rows.Err() != nil {
		return version, err
	}

	return version, err
}
