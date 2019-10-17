package dbadmin

import "io"

// TableName is an alias for string to make function signatures more expressive
type TableName string

// DbAdmin contains the methods that are used to introspect runtime state
// and control access to a database
type DbAdmin interface {
	io.Closer

	// WriteCredentials will add a username to the database with the given password
	WriteCredentials(username, password string) error

	// ListUsernames will return a list of all usernames in the database with
	// the given prefix.
	ListUsernames(usernamePrefix string) ([]string, error)

	// VerifyUnusedAndDeleteCredentials will ensure that there are no current
	// connections using the specified username, and then delete the user.
	// If there is an active connection using the credentials an error will be
	// returned.
	VerifyUnusedAndDeleteCredentials(username string) error

	// GetSchemaVersion will return the current version of the database, usually
	// as decoded by a MigrationEngine instance.
	GetSchemaVersion() (string, error)

	// GetTableSizeEstimates will return the estimated number of rows in the
	// specified tables.
	GetTableSizeEstimates(tableNames []TableName) (map[TableName]uint64, error)

	// GetNextIDs will return the next ID that will be assigned to a new row in
	// the specified tables.
	GetNextIDs(tableNames []TableName) (map[TableName]uint64, error)

	// SelectFloat will select a single value out of a single row and return
	// only that value as a float.
	SelectFloat(selectQuery string) (float64, error)
}

// MigrationEngine is an interface for deciphering the bookkeeping information
// stored by a particular migration framework from within a database
type MigrationEngine interface {
	// GetVersionQuery will return the SQL query that should be run against a
	// database to return a single string, which represents the current version
	// of the database.
	GetVersionQuery() string
}
