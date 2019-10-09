package mysqladmin

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"

	mapset "github.com/deckarep/golang-set"
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

// GetTableSizeEstimates implements DbAdmin
func (mdba *MySQLDbAdmin) GetTableSizeEstimates(tableNames []dbadmin.TableName) (map[dbadmin.TableName]uint64, error) {
	estimates := make(map[dbadmin.TableName]uint64)

	// Pre-allocate the map with zeroes for each table
	for _, tableName := range tableNames {
		estimates[tableName] = 0
	}

	if len(tableNames) == 0 {
		return estimates, nil
	}

	query, args, err := sqlx.In("SELECT TABLE_NAME, TABLE_ROWS FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME IN (?);", tableNames)
	if err != nil {
		return estimates, fmt.Errorf("Unable to prepare query to load table size estimates: %w", err)
	}
	query = mdba.handle.Rebind(query)

	var results []struct {
		TableName string `db:"TABLE_NAME"`
		TableRows uint64 `db:"TABLE_ROWS"`
	}
	if err := mdba.handle.Select(&results, query, args...); err != nil {
		return estimates, wrap(err)
	}

	for _, result := range results {
		estimates[dbadmin.TableName(result.TableName)] = result.TableRows
	}

	return estimates, nil
}

// GetNextIDs implements DbAdmin
func (mdba *MySQLDbAdmin) GetNextIDs(tableNames []dbadmin.TableName) (map[dbadmin.TableName]uint64, error) {
	nextIDs := make(map[dbadmin.TableName]uint64)

	if len(tableNames) == 0 {
		return nextIDs, nil
	}

	query, args, err := sqlx.In("SELECT TABLE_NAME, AUTO_INCREMENT FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME IN (?);", tableNames)
	if err != nil {
		return nextIDs, fmt.Errorf("Unable to prepare query to load table size nextIDs: %w", err)
	}
	query = mdba.handle.Rebind(query)

	var results []struct {
		TableName     string `db:"TABLE_NAME"`
		AutoIncrement uint64 `db:"AUTO_INCREMENT"`
	}
	if err := mdba.handle.Select(&results, query, args...); err != nil {
		return nextIDs, wrap(err)
	}

	if len(tableNames) != len(results) {
		return nextIDs, fmt.Errorf("Unable to load table nextIDs for all tables, expected %d, got %d", len(tableNames), len(results))
	}

	for _, result := range results {
		nextIDs[dbadmin.TableName(result.TableName)] = result.AutoIncrement
	}

	return nextIDs, nil
}

// SelectFloat implements DbAdmin
func (mdba *MySQLDbAdmin) SelectFloat(selectQuery string) (result float64, _ error) {
	if err := mdba.handle.Get(&result, selectQuery); err != nil {
		return result, wrap(err)
	}

	return result, nil
}

// IsBlockingIndexCreation implements DbAdmin
func (mdba *MySQLDbAdmin) IsBlockingIndexCreation(tableName dbadmin.TableName, indexType dbadmin.IndexType, columns ...string) (bool, error) {
	return false, fmt.Errorf("Not implemented")
}

// ConstraintWillFail implements DbAdmin
func (mdba *MySQLDbAdmin) ConstraintWillFail(tableName dbadmin.TableName, constraintType dbadmin.ConstraintType, columns ...string) (bool, error) {
	switch constraintType {
	case dbadmin.NotNullConstraint:
		// Find out if the table already has a not null constraint on all of the named columns
		query, args, err := sqlx.In("SHOW COLUMNS FROM `?` WHERE Field IN (?);", tableName, columns)
		if err != nil {
			return true, fmt.Errorf("Unable to prepare query to read table definition: %w", err)
		}
		query = mdba.handle.Rebind(query)

		var fieldDefinitions []struct {
			Field string
			Type  string
			Null  string
		}
		if err := mdba.handle.Select(&fieldDefinitions, query, args...); err != nil {
			return true, wrap(err)
		}

		fieldNullableMap := make(map[string]string, len(fieldDefinitions))
		for _, fieldDefinition := range fieldDefinitions {
			fieldNullableMap[fieldDefinition.Field] = fieldDefinition.Null
		}

		for _, columnToCheck := range columns {
			definition, ok := fieldNullableMap[columnToCheck]
			if !ok {
				// Will definitely fail because the column doesn't exist
				return true, nil
			}

			if definition == "YES" {
				// The table currently allows for nulls in the column, and we're
				// trying to make it not nullable. Check if there are any nulls
				// in that column.
				query := "SELECT * FROM `?` WHERE `?` NOT NULL LIMIT 1;"
				_, err := mdba.handle.QueryRowx(query, tableName, columnToCheck).SliceScan()
				if err != sql.ErrNoRows {
					if err != nil {
						return true, wrap(err)
					}

					// A value was returned, meaning there are null rows in this column
					return true, nil
				}
			}
		}
		return false, nil
	case dbadmin.UniqueConstraint:
		listUniqueIndexQuery := "SHOW INDEX FROM `?` WHERE Non_unique = 0;"

		var uniqueIndexRows []struct {
			KeyName    string `db:"Key_name"`
			ColumnName string `db:"Column_name"`
		}
		if err := mdba.handle.Select(&uniqueIndexRows, listUniqueIndexQuery, tableName); err != nil {
			return true, wrap(err)
		}

		columnsByIndex := make(map[string]mapset.Set)
		for _, uniqueIndexRow := range uniqueIndexRows {
			indexColumnSet, ok := columnsByIndex[uniqueIndexRow.KeyName]
			if !ok {
				indexColumnSet = mapset.NewSet()
				columnsByIndex[uniqueIndexRow.KeyName] = indexColumnSet
			}

			indexColumnSet.Add(uniqueIndexRow.ColumnName)
		}

		newConstraintColumnSet := mapset.NewSet()
		for _, columnName := range columns {
			newConstraintColumnSet.Add(columnName)
		}
		for _, existingIndexColumnSet := range columnsByIndex {
			if existingIndexColumnSet.Equal(newConstraintColumnSet) {
				// We can add this constraint because there is already a unique
				// index over the same columns
				return false, nil
			}
		}

		// There is no existing index, we must check if there is some value in
		// the database which violates the constraint
		query, args, err := sqlx.In("SELECT COUNT(*) FROM `?` GROUP BY ? HAVING COUNT(*) > 1 LIMIT 1;", tableName, columns)
		if err != nil {
			return true, fmt.Errorf("Unable to prepare query to search for duplicate data: %w", err)
		}
		query = mdba.handle.Rebind(query)

		_, err = mdba.handle.QueryRowx(query, args...).SliceScan()
		if err != sql.ErrNoRows {
			if err != nil {
				return true, wrap(err)
			}

			// A value was returned, meaning there are duplicate rows
			return true, nil
		}

		return false, nil
	default:
		return false, fmt.Errorf("Unknown constraint type: %d", constraintType)
	}
}

// Close implements DbAdmin
func (mdba *MySQLDbAdmin) Close() error {
	return mdba.handle.Close()
}
