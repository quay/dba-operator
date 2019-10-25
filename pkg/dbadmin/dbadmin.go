package dbadmin

import "io"

// TableName is an alias for string to make function signatures more expressive
type TableName string

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 . DbAdmin

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
	//
	// If a table does not exist, the map will contain the value zero for that
	// table.
	GetTableSizeEstimates(tableNames []TableName) (map[TableName]uint64, error)

	// GetNextIDs will return the next ID that will be assigned to a new row in
	// the specified tables.
	GetNextIDs(tableNames []TableName) (map[TableName]uint64, error)

	// SelectFloat will select a single value out of a single row and return
	// only that value as a float.
	SelectFloat(selectQuery string) (float64, error)

	// IsBlockingIndexCreation will determine whether adding an index to the
	// specified table will cause writes to
	IsBlockingIndexCreation(tableName TableName, indexType IndexType, columns ...string) (bool, error)

	// AddConstraintWillFail returns true if adding the constraint will fail due
	// to the data itself.
	ConstraintWillFail(tableName TableName, constraintType ConstraintType, columns ...string) (bool, error)
}

// IndexType defines the varioues types of indexes we will evaluate for being
// applied to the database
type IndexType int

const (
	// Index is a regular single column or compound index.
	Index IndexType = iota

	// PrimaryKeyIndex is an index that is unique and implicitly applied on
	// primary key fields.
	PrimaryKeyIndex

	// UniqueIndex indexes enforce that rows contain unique values for the
	// indexed column(s). Null values are considered as non-matching and can be
	// present and repeated.
	UniqueIndex

	// FulltextIndex indexes aid in the process of full-text searching.
	FulltextIndex

	// SpatialIndex indexes represent multi-dimensional location based data.
	SpatialIndex
)

// ConstraintType defines the various types of constraints we can evaluate for
// being applied to a database.
type ConstraintType int

const (
	// NotNullConstraint guarantees that a column will not contain any `NULL`
	// values
	NotNullConstraint ConstraintType = iota

	// UniqueConstraint requires that all rows in a column have unique values
	// over the columns specified.
	UniqueConstraint
)

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 . MigrationEngine

// MigrationEngine is an interface for deciphering the bookkeeping information
// stored by a particular migration framework from within a database
type MigrationEngine interface {
	// GetVersionQuery will return the SQL query that should be run against a
	// database to return a single string, which represents the current version
	// of the database.
	GetVersionQuery() string
}
