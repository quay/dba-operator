package mysqladmin

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/app-sre/dba-operator/pkg/dbadmin"
	"github.com/app-sre/dba-operator/pkg/xerrors"
)

// MySQLDbAdmin is a type which implements DbAdmin for MySQL databases
type MySQLDbAdmin struct {
	handle   *sqlx.DB
	database string
	engine   dbadmin.MigrationEngine
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

// CreateMySQLAdmin will instantiate a MySQLDbAdmin object with the specified
// connection information and MigrationEngine.
func CreateMySQLAdmin(dsn string, engine dbadmin.MigrationEngine) (dbadmin.DbAdmin, error) {
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("Unable to parse connection dsn: %w", err)
	}
	if parsed.User == "" || parsed.Passwd == "" {
		return nil, errors.New("Must provide username and password in the connection DSN")
	}
	if parsed.DBName == "" {
		return nil, errors.New("Must provide specific database name in the connection DSN")
	}

	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("Unable to open connection to db: %w", wrap(err))
	}

	return &MySQLDbAdmin{db, parsed.DBName, engine}, nil
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
func (mdba *MySQLDbAdmin) indirectSubstitute(format string, args ...sqlValue) xerrors.EnhancedError {
	tx, err := mdba.handle.Begin()
	if err != nil {
		return wrap(err)
	}
	defer tx.Rollback() // nolint: errcheck

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
			return wrap(err)
		}
	}

	rawSQLStmt := fmt.Sprintf(format, finalArgs...)
	stmtStringName := randIdentifier(16)
	createStmt := fmt.Sprintf(`SET @%s := CONCAT("%s")`, stmtStringName, rawSQLStmt)
	_, err = tx.Exec(createStmt)
	if err != nil {
		return wrap(err)
	}

	stmtName := randIdentifier(16)
	_, err = tx.Exec(fmt.Sprintf("PREPARE %s FROM @%s", stmtName, stmtStringName))
	if err != nil {
		return wrap(err)
	}

	_, err = tx.Exec(fmt.Sprintf("EXECUTE %s", stmtName))
	if err != nil {
		return wrap(err)
	}

	if err := tx.Commit(); err != nil {
		return wrap(err)
	}

	return nil
}

// WriteCredentials implements DbADmin
func (mdba *MySQLDbAdmin) WriteCredentials(username, password string) error {

	err := mdba.indirectSubstitute(
		"CREATE USER %s@'%%' IDENTIFIED BY %s",
		quoted(username),
		quoted(password),
	)
	if err != nil {
		return fmt.Errorf("Unable to create new user %s: %w", username, err)
	}

	err = mdba.indirectSubstitute(
		"GRANT SELECT, INSERT, UPDATE, DELETE ON %s.* TO %s",
		noquote(mdba.database),
		quoted(username),
	)
	if err != nil {
		return fmt.Errorf("Unable to grant permission to new user %s: %w", username, wrap(err))
	}

	return nil
}

// ListUsernames implements DbADmin
func (mdba *MySQLDbAdmin) ListUsernames(usernamePrefix string) ([]string, error) {
	var usernames []string
	if err := mdba.handle.Select(
		&usernames,
		"SELECT user FROM mysql.user WHERE user LIKE ?",
		usernamePrefix+"%",
	); err != nil {
		return []string{}, fmt.Errorf("Unable to list existing usernames: %w", wrap(err))
	}

	return usernames, nil
}

// VerifyUnusedAndDeleteCredentials implements DbAdmin
func (mdba *MySQLDbAdmin) VerifyUnusedAndDeleteCredentials(username string) error {
	var sessionCount int
	if err := mdba.handle.Get(
		&sessionCount,
		"SELECT COUNT(*) FROM information_schema.processlist WHERE user = ?",
		username,
	); err != nil {
		return fmt.Errorf("Unable to query or parse session count for user %s: %w", username, wrap(err))
	}

	if sessionCount > 0 {
		return xerrors.NewTempErrorf("Unable to remove user %s, %d active sessions remaining", username, sessionCount)
	}

	if err := mdba.indirectSubstitute(
		"DROP USER %s",
		quoted(username),
	); err != nil {
		return fmt.Errorf("Unable to remove user %s from the database: %w", username, err)
	}

	return nil
}

// GetSchemaVersion implements DbAdmin
func (mdba *MySQLDbAdmin) GetSchemaVersion() (string, error) {
	var version string
	if err := mdba.handle.Get(&version, mdba.engine.GetVersionQuery()); err != nil {
		mysqlErr, ok := err.(*mysql.MySQLError)
		if ok && mysqlErr.Number == 1146 {
			// No migration engine metadata, likely an empty database
			return "", nil
		}
		return "", wrap(err)
	}

	return version, nil
}

// Close implements DbADmin
func (mdba *MySQLDbAdmin) Close() error {
	return mdba.handle.Close()
}
