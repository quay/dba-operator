package dbadmin

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/xo/dburl"
)

type engineInit func(*sql.DB, string, MigrationEngine) (DbAdmin, error)

var engines = make(map[string]engineInit)

// Register is used to notify that there is a new DbAdmin compatible engine.
func Register(driverName string, initFunc engineInit) {
	engines[driverName] = initFunc
}

// Open will use the specified DSN to create a new engine-specifc DbAdmin
// database connection.
func Open(dsn string, migrationEngine MigrationEngine) (DbAdmin, error) {
	parsed, err := dburl.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("Unable to parse connection dsn: %w", err)
	}

	if parsed.User.Username() == "" {
		return nil, errors.New("Must provide username in the connection DSN")
	}
	_, passwordSet := parsed.User.Password()
	if !passwordSet {
		return nil, errors.New("Must provide password in the connection DSN")
	}

	if len(parsed.Path) <= 1 {
		return nil, errors.New("Must provide specific database name in the connection DSN")
	}
	dbName := parsed.Path[1:]

	handle, err := dburl.Open(dsn)
	if err != nil {
		return nil, fmt.Errorf("Unable to instantiate engine: %w", err)
	}

	initFunc, ok := engines[parsed.Driver]
	if !ok {
		return nil, fmt.Errorf("Driver (%s) is not registered, please import conforming driver", parsed.Driver)
	}

	return initFunc(handle, dbName, migrationEngine)
}
