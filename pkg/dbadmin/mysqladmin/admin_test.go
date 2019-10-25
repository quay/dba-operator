package mysqladmin

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"

	"github.com/app-sre/dba-operator/pkg/dbadmin"
	"github.com/app-sre/dba-operator/pkg/dbadmin/dbadminfakes"
	"github.com/app-sre/dba-operator/pkg/xerrors"
)

const (
	fakeDBName   = "fakedb"
	fakeVersion  = "123"
	fakeUsername = "fakeUser"
	fakePassword = "fakePassword"
	versionQuery = "SELECT version_num FROM version_table LIMIT 1"
)

var emptyResult = sqlmock.NewResult(0, 0)

func createMockDB(t *testing.T) (dbadmin.DbAdmin, sqlmock.Sqlmock) {
	mockDB, mock, err := sqlmock.New()
	assert.NoError(t, err, "Sqlmock should not raise an error")
	handle := sqlx.NewDb(mockDB, "mysql")

	migrationEngine := dbadminfakes.FakeMigrationEngine{}
	migrationEngine.GetVersionQueryReturns(versionQuery)
	return &MySQLDbAdmin{handle, fakeDBName, &migrationEngine}, mock
}

func TestGetSchemaVersion(t *testing.T) {
	assert := assert.New(t)
	db, mock := createMockDB(t)
	mock.ExpectQuery(versionQuery).WillReturnRows(mock.NewRows([]string{"version_num"}).AddRow(fakeVersion))

	currentVersion, err := db.GetSchemaVersion()
	assert.NoError(err)
	assert.Equal(fakeVersion, currentVersion)
}

func TestGetSchemaVersionEmptyDB(t *testing.T) {
	assert := assert.New(t)
	db, mock := createMockDB(t)
	mock.ExpectQuery(versionQuery).WillReturnError(&mysql.MySQLError{Number: 1146, Message: "Unknown table"})

	currentVersion, err := db.GetSchemaVersion()
	assert.NoError(err)
	assert.Equal("", currentVersion)
}

var retryableErrorTests = []struct {
	err       error
	retryable bool
}{
	// Positive cases
	{&mysql.MySQLError{Number: 1020, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1020, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1036, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1040, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1043, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1053, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1105, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1129, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1180, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1181, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1182, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1188, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1202, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1203, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1205, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1206, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1218, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1220, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1290, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1297, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1317, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1637, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1836, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 1874, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 3019, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 3032, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 3168, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 3169, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 3186, Message: "0xdeadbeef"}, true},
	{&mysql.MySQLError{Number: 3572, Message: "0xdeadbeef"}, true},
	{mysql.ErrInvalidConn, true},
	{mysql.ErrMalformPkt, true},
	{mysql.ErrPktSync, true},
	{mysql.ErrPktSyncMul, true},
	{mysql.ErrBusyBuffer, true},

	// Negative cases
	{mysql.ErrUnknownPlugin, false},
	{&mysql.MySQLError{Number: 1000, Message: "0xdeadbeef"}, false},
	{&mysql.MySQLError{Number: 0, Message: "0xdeadbeef"}, false},
}

func TestRetryableError(t *testing.T) {
	assert := assert.New(t)
	db, mock := createMockDB(t)

	for _, errorTest := range retryableErrorTests {
		mock.ExpectQuery(versionQuery).WillReturnError(errorTest.err)
		_, err := db.GetSchemaVersion()
		assert.Error(err)

		var maybeTemporary xerrors.EnhancedError
		errors.As(err, &maybeTemporary)

		isTemporary := false
		if maybeTemporary != nil {
			isTemporary = maybeTemporary.Temporary()
		}
		assert.Equal(errorTest.retryable, isTemporary)
	}
}

func TestWriteCredentials(t *testing.T) {
	assert := assert.New(t)
	db, mock := createMockDB(t)

	mock.ExpectBegin()
	mock.ExpectExec(`^SET @(\S+) := ?`).WithArgs(fakeUsername).WillReturnResult(emptyResult)
	mock.ExpectExec(`^SET @(\S+) := ?`).WithArgs(fakePassword).WillReturnResult(emptyResult)
	mock.ExpectExec(`^SET @(\S+) := CONCAT\("CREATE USER ", QUOTE\([^\)]+\), "@'%' IDENTIFIED BY ", QUOTE\([^\)]+\), ""\)`).WillReturnResult(emptyResult)
	mock.ExpectExec(`^PREPARE (\S+) FROM (\S+)`).WillReturnResult(emptyResult)
	mock.ExpectExec(`EXECUTE (\S+)`).WillReturnResult(emptyResult)
	mock.ExpectCommit()

	mock.ExpectBegin()
	mock.ExpectExec(`^SET @(\S+) := ?`).WithArgs(fakeDBName).WillReturnResult(emptyResult)
	mock.ExpectExec(`^SET @(\S+) := ?`).WithArgs(fakeUsername).WillReturnResult(emptyResult)
	mock.ExpectExec(`^SET @(\S+) := CONCAT\("GRANT SELECT, INSERT, UPDATE, DELETE ON ", @(\S+), "\.\* TO ", QUOTE\(@(\S+)\), ""\)`).WillReturnResult(emptyResult)
	mock.ExpectExec(`^PREPARE (\S+) FROM (\S+)`).WillReturnResult(emptyResult)
	mock.ExpectExec(`EXECUTE (\S+)`).WillReturnResult(emptyResult)
	mock.ExpectCommit()

	assert.NoError(db.WriteCredentials(fakeUsername, fakePassword))
}

func TestWriteCredentialsConflict(t *testing.T) {
	assert := assert.New(t)
	db, mock := createMockDB(t)

	mock.ExpectBegin()
	mock.ExpectExec(`^SET @(\S+) := ?`).WithArgs(fakeUsername).WillReturnResult(emptyResult)
	mock.ExpectExec(`^SET @(\S+) := ?`).WithArgs(fakePassword).WillReturnResult(emptyResult)
	mock.ExpectExec(`^SET @(\S+) := CONCAT\("CREATE USER ", QUOTE\([^\)]+\), "@'%' IDENTIFIED BY ", QUOTE\([^\)]+\), ""\)`).WillReturnResult(emptyResult)
	mock.ExpectExec(`^PREPARE (\S+) FROM (\S+)`).WillReturnResult(emptyResult)
	expectedError := errors.New("username already exists")
	mock.ExpectExec(`EXECUTE (\S+)`).WillReturnError(expectedError)
	mock.ExpectRollback()

	err := db.WriteCredentials(fakeUsername, fakePassword)
	assert.Truef(errors.Is(err, expectedError), "Only the expected error should be returned, actual error: %v", err)
}

func TestListUsernames(t *testing.T) {
	assert := assert.New(t)
	db, mock := createMockDB(t)

	rows := mock.NewRows([]string{"user"}).AddRow("dba123").AddRow("dba456")
	mock.ExpectQuery("^SELECT user FROM mysql.user WHERE user LIKE ?").WithArgs("dba%").WillReturnRows(rows)

	usernames, err := db.ListUsernames("dba")
	assert.NoError(err)
	assert.Equal(2, len(usernames))
}

func TestDeleteCredentialsUnused(t *testing.T) {
	assert := assert.New(t)
	db, mock := createMockDB(t)

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM information_schema.processlist WHERE user = ?`).
		WithArgs(fakeUsername).
		WillReturnRows(mock.NewRows([]string{"count"}).AddRow(0))

	mock.ExpectBegin()
	mock.ExpectExec(`^SET @(\S+) := ?`).WithArgs(fakeUsername).WillReturnResult(emptyResult)
	mock.ExpectExec(`^SET @(\S+) := CONCAT\("DROP USER ", QUOTE\([^\)]+\), ""\)`).WillReturnResult(emptyResult)
	mock.ExpectExec(`^PREPARE (\S+) FROM (\S+)`).WillReturnResult(emptyResult)
	mock.ExpectExec(`EXECUTE (\S+)`).WillReturnResult(emptyResult)
	mock.ExpectCommit()

	err := db.VerifyUnusedAndDeleteCredentials(fakeUsername)
	assert.NoError(err)
}

func TestDeleteCredentialsStillUsed(t *testing.T) {
	assert := assert.New(t)
	db, mock := createMockDB(t)

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM information_schema.processlist WHERE user = ?`).
		WithArgs(fakeUsername).
		WillReturnRows(mock.NewRows([]string{"count"}).AddRow(1))

	err := db.VerifyUnusedAndDeleteCredentials(fakeUsername)

	var maybeTemporary xerrors.EnhancedError
	assert.True(errors.As(err, &maybeTemporary))
	assert.True(maybeTemporary.Temporary())
}

func TestGetTableSizeEstimates(t *testing.T) {
	assert := assert.New(t)
	db, mock := createMockDB(t)

	table1 := dbadmin.TableName("fakeTable1")
	mock.ExpectQuery(`SELECT TABLE_NAME, TABLE_ROWS FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME IN \(\?\);`).
		WithArgs(table1).
		WillReturnRows(mock.NewRows([]string{"TABLE_NAME", "TABLE_ROWS"}).AddRow(table1, 50))

	estimates, err := db.GetTableSizeEstimates([]dbadmin.TableName{table1})
	assert.NoError(err)

	rowCount, ok := estimates[table1]
	assert.True(ok)
	assert.Equal(uint64(50), rowCount)

	_, notOk := estimates[dbadmin.TableName("notARealTable")]
	assert.False(notOk)
}

func TestGetTableSizeEstimatesMultiple(t *testing.T) {
	assert := assert.New(t)
	db, mock := createMockDB(t)

	table1 := dbadmin.TableName("fakeTable1")
	table2 := dbadmin.TableName("fakeTable2")
	mock.ExpectQuery(`SELECT TABLE_NAME, TABLE_ROWS FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME IN \(\?, \?\);`).
		WithArgs(table1, table2).
		WillReturnRows(mock.NewRows([]string{"TABLE_NAME", "TABLE_ROWS"}).AddRow(table1, 50).AddRow(table2, 100))

	estimates, err := db.GetTableSizeEstimates([]dbadmin.TableName{table1, table2})
	assert.NoError(err)

	rowCount, ok := estimates[table1]
	assert.True(ok)
	assert.Equal(uint64(50), rowCount)

	rowCount, ok = estimates[table2]
	assert.True(ok)
	assert.Equal(uint64(100), rowCount)
}

func TestGetTableSizeEstimatesMissingTables(t *testing.T) {
	assert := assert.New(t)
	db, mock := createMockDB(t)

	table1 := dbadmin.TableName("fakeTable1")
	table2 := dbadmin.TableName("fakeTable2")
	mock.ExpectQuery(`SELECT TABLE_NAME, TABLE_ROWS FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME IN \(\?, \?\);`).
		WithArgs(table1, table2).
		WillReturnRows(mock.NewRows([]string{"TABLE_NAME", "TABLE_ROWS"}).AddRow(table1, 50))

	estimates, err := db.GetTableSizeEstimates([]dbadmin.TableName{table1, table2})
	assert.NoError(err)
	assert.Equal(uint64(50), estimates[table1])
	assert.Equal(uint64(0), estimates[table2])
}

func TestGetNextIDs(t *testing.T) {
	assert := assert.New(t)
	db, mock := createMockDB(t)

	table1 := dbadmin.TableName("fakeTable1")
	mock.ExpectQuery(`SELECT TABLE_NAME, AUTO_INCREMENT FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME IN \(\?\);`).
		WithArgs(table1).
		WillReturnRows(mock.NewRows([]string{"TABLE_NAME", "AUTO_INCREMENT"}).AddRow(table1, 50))

	estimates, err := db.GetNextIDs([]dbadmin.TableName{table1})
	assert.NoError(err)

	rowCount, ok := estimates[table1]
	assert.True(ok)
	assert.Equal(uint64(50), rowCount)

	_, notOk := estimates[dbadmin.TableName("notARealTable")]
	assert.False(notOk)
}

func TestGetNextIDsMultiple(t *testing.T) {
	assert := assert.New(t)
	db, mock := createMockDB(t)

	table1 := dbadmin.TableName("fakeTable1")
	table2 := dbadmin.TableName("fakeTable2")
	mock.ExpectQuery(`SELECT TABLE_NAME, AUTO_INCREMENT FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME IN \(\?, \?\);`).
		WithArgs(table1, table2).
		WillReturnRows(mock.NewRows([]string{"TABLE_NAME", "AUTO_INCREMENT"}).AddRow(table1, 50).AddRow(table2, 100))

	estimates, err := db.GetNextIDs([]dbadmin.TableName{table1, table2})
	assert.NoError(err)

	rowCount, ok := estimates[table1]
	assert.True(ok)
	assert.Equal(uint64(50), rowCount)

	rowCount, ok = estimates[table2]
	assert.True(ok)
	assert.Equal(uint64(100), rowCount)
}

func TestGetNextIDsMissingTables(t *testing.T) {
	assert := assert.New(t)
	db, mock := createMockDB(t)

	table1 := dbadmin.TableName("fakeTable1")
	table2 := dbadmin.TableName("fakeTable2")
	mock.ExpectQuery(`SELECT TABLE_NAME, AUTO_INCREMENT FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME IN \(\?, \?\);`).
		WithArgs(table1, table2).
		WillReturnRows(mock.NewRows([]string{"TABLE_NAME", "AUTO_INCREMENT"}).AddRow(table1, 50))

	_, err := db.GetNextIDs([]dbadmin.TableName{table1, table2})
	assert.Error(err)
}

func TestSelectFloat(t *testing.T) {
	assert := assert.New(t)
	db, mock := createMockDB(t)

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM tableName`).
		WillReturnRows(mock.NewRows([]string{"count"}).AddRow(1000))

	floatVal, err := db.SelectFloat(`SELECT COUNT(*) FROM tableName`)
	assert.NoError(err)
	assert.Equal(1000.0, floatVal)
}

func TestClose(t *testing.T) {
	assert := assert.New(t)
	db, mock := createMockDB(t)

	mock.ExpectClose()

	assert.NoError(db.Close())
}
