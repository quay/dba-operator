package dbadmin

type DbAdmin interface {
	WriteCredentials(username, password string) error

	ListUsernames(usernamePrefix string) ([]string, error)

	VerifyUnusedAndDeleteCredentials(username string) error

	GetSchemaVersion() (string, error)
}

type MigrationMetadata interface {
	GetVersionQuery() string
}
