package dbadmin

type DbAdmin interface {
	WriteCredentials(username, password string) error

	VerifyUnusedAndDeleteCredentials(username string) error

	GetSchemaVersion() (string, error)
}

type MigrationMetadata interface {
	GetVersionQuery() string
}
