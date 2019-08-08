package mysqladmin

import (
	"database/sql"
	"fmt"

	"github.com/go-sql-driver/mysql"

	"github.com/app-sre/dba-operator/pkg/dbadmin"
)

type MySQLDbAdmin struct {
	handle   *sql.DB
	database string
	metadata dbadmin.MigrationMetadata
}

func CreateMySQLAdmin(username, password, hostname string, port uint16, database string, metadata dbadmin.MigrationMetadata) (dbadmin.DbAdmin, error) {
	connectionString := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", username, password, hostname, port, database)
	db, err := sql.Open("mysql", connectionString)

	return &MySQLDbAdmin{db, database, metadata}, err
}

func (mdba *MySQLDbAdmin) WriteCredentials(username, password string) error {

	tx, err := mdba.handle.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("SET @username := ?", username)
	if err != nil {
		return err
	}

	_, err = tx.Exec("SET @password := ?", password)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`SET @createStmt := CONCAT("CREATE USER ", QUOTE(@username), "@'%' IDENTIFIED BY ", QUOTE(@password))`)
	if err != nil {
		return err
	}

	_, err = tx.Exec("PREPARE stmt FROM @createStmt")
	if err != nil {
		return err
	}

	_, err = tx.Exec("EXECUTE stmt")
	if err != nil {
		return err
	}

	_, err = tx.Exec("SET @dbname := ?", mdba.database)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`SET @grantStmt := CONCAT("GRANT SELECT, INSERT, UPDATE, DELETE ON ", @dbname, ".* TO ", QUOTE(@username))`)
	if err != nil {
		return err
	}

	_, err = tx.Exec("PREPARE grantnow FROM @grantStmt")
	if err != nil {
		return err
	}

	_, err = tx.Exec("EXECUTE grantnow")
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (mdba *MySQLDbAdmin) VerifyUnusedAndDeleteCredentials(username string) error {
	sessionCountRow := mdba.handle.QueryRow("SELECT COUNT(*) FROM information_schema.processlist WHERE user = ?", username)

	var sessionCount int
	err := sessionCountRow.Scan(&sessionCount)
	if err != nil {
		return err
	}

	if sessionCount > 0 {
		return fmt.Errorf("Unable to remove user %s, %d active sessions remaining", username, sessionCount)
	}

	tx, err := mdba.handle.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("SET @username := ?", username)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`SET @dropUser := CONCAT("DROP USER ", QUOTE(@username))`)
	if err != nil {
		return err
	}

	_, err = tx.Exec("PREPARE dropnow FROM @dropUser")
	if err != nil {
		return err
	}

	_, err = tx.Exec("EXECUTE dropnow")
	if err != nil {
		return err
	}

	return tx.Commit()
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
