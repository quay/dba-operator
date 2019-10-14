package dbadmin

// DbAdmin contains the methods that are used to introspect runtime state
// and control access to a database
type DbAdmin interface {
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

	// Close the connection.
	Close() error
}

// MigrationEngine is an interface for deciphering the bookkeeping information
// stored by a particular migration framework from within a database
type MigrationEngine interface {
	// GetVersionQuery will return the SQL query that should be run against a
	// database to return a single string, which represents the current version
	// of the database.
	GetVersionQuery() string
}
