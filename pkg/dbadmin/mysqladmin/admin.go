package mysqladmin

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"

	"github.com/go-sql-driver/mysql"

	"github.com/app-sre/dba-operator/pkg/dbadmin"
)

type MySQLDbAdmin struct {
	handle   *sql.DB
	database string
	metadata dbadmin.MigrationMetadata
}

type sqlValue struct {
	value  *string
	quoted bool
}

func quoted(needsToBeQuoted string) sqlValue {
	return sqlValue{value: &needsToBeQuoted, quoted: true}
}

func noquote(cantBeQuoted string) sqlValue {
	return sqlValue{value: &cantBeQuoted, quoted: false}
}

func CreateMySQLAdmin(dsn string, metadata dbadmin.MigrationMetadata) (dbadmin.DbAdmin, error) {
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	if parsed.User == "" || parsed.Passwd == "" {
		return nil, errors.New("Must provide username and password in the connection DSN")
	}
	if parsed.DBName == "" {
		return nil, errors.New("Must provide specific database name in the connection DSN")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	return &MySQLDbAdmin{db, parsed.DBName, metadata}, err
}

func randIdentifier(randomBytes int) string {
	identBytes := make([]byte, randomBytes)
	rand.Read(identBytes)

	// Here we prepend "var" to handle an edge case where some hex (e.g. 1e2)
	// gets interpreted as scientific notation by MySQL
	return "var" + hex.EncodeToString(identBytes)
}

// This method attempts to prevent sql injection on MySQL DBMS control commands
// such as CREATE USER and GRANT which don't support variables in prepared statements.
// The design of this operator shouldn't require preventing injection as these values
// are developer supplied and not end-user supplied, but it may help prevent errors
// and should be considered a best practice.
func (mdba *MySQLDbAdmin) indirectSubstitute(format string, args ...sqlValue) error {
	tx, err := mdba.handle.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	finalArgs := make([]interface{}, 0, len(args))
	for _, arg := range args {
		newIdent := randIdentifier(16)

		if arg.quoted {
			finalArgs = append(finalArgs, fmt.Sprintf(`", QUOTE(@%s), "`, newIdent))
		} else {
			finalArgs = append(finalArgs, fmt.Sprintf(`", @%s, "`, newIdent))
		}

		_, err = tx.Exec(fmt.Sprintf("SET @%s := ?", newIdent), arg.value)
		if err != nil {
			return err
		}
	}

	rawSQLStmt := fmt.Sprintf(format, finalArgs...)
	stmtStringName := randIdentifier(16)
	createStmt := fmt.Sprintf(`SET @%s := CONCAT("%s")`, stmtStringName, rawSQLStmt)
	_, err = tx.Exec(createStmt)
	if err != nil {
		return err
	}

	stmtName := randIdentifier(16)
	_, err = tx.Exec(fmt.Sprintf("PREPARE %s FROM @%s", stmtName, stmtStringName))
	if err != nil {
		return err
	}

	_, err = tx.Exec(fmt.Sprintf("EXECUTE %s", stmtName))
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (mdba *MySQLDbAdmin) WriteCredentials(username, password string) error {

	err := mdba.indirectSubstitute(
		"CREATE USER %s@'%%' IDENTIFIED BY %s",
		quoted(username),
		quoted(password),
	)
	if err != nil {
		return err
	}

	return mdba.indirectSubstitute(
		"GRANT SELECT, INSERT, UPDATE, DELETE ON %s.* TO %s",
		noquote(mdba.database),
		quoted(username),
	)
}

func (mdba *MySQLDbAdmin) VerifyUnusedAndDeleteCredentials(username string) error {
	sessionCountRow := mdba.handle.QueryRow(
		"SELECT COUNT(*) FROM information_schema.processlist WHERE user = ?",
		username,
	)

	var sessionCount int
	err := sessionCountRow.Scan(&sessionCount)
	if err != nil {
		return err
	}

	if sessionCount > 0 {
		return fmt.Errorf("Unable to remove user %s, %d active sessions remaining", username, sessionCount)
	}

	return mdba.indirectSubstitute(
		"DROP USER %s",
		quoted(username),
	)
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
