package hints

import (
	"testing"

	dba "github.com/app-sre/dba-operator/api/v1alpha1"
	"github.com/app-sre/dba-operator/pkg/dbadmin"
	"github.com/app-sre/dba-operator/pkg/dbadmin/dbadminfakes"
	"github.com/stretchr/testify/assert"
)

const (
	fakeTableName        = dbadmin.TableName("fakeTable")
	fakeColumn           = "fakeColumn"
	largeTableDefinition = 1_000_000
)

var addColumnTests = []struct {
	hint              dba.DatabaseMigrationSchemaHintColumn
	tableSize         uint64
	numExpectedErrors int
}{
	{
		dba.DatabaseMigrationSchemaHintColumn{
			NotNullable:      false,
			HasServerDefault: false,
		}, 0, 0,
	},
	{
		dba.DatabaseMigrationSchemaHintColumn{
			NotNullable:      true,
			HasServerDefault: false,
		}, 0, 1,
	},
	{
		dba.DatabaseMigrationSchemaHintColumn{
			NotNullable:      false,
			HasServerDefault: true,
		}, 0, 0,
	},
	{
		dba.DatabaseMigrationSchemaHintColumn{
			NotNullable:      true,
			HasServerDefault: true,
		}, 0, 0,
	},
	{
		dba.DatabaseMigrationSchemaHintColumn{
			NotNullable:      false,
			HasServerDefault: false,
		}, 1, 0,
	},
	{
		dba.DatabaseMigrationSchemaHintColumn{
			NotNullable:      true,
			HasServerDefault: false,
		}, 1, 1,
	},
	{
		dba.DatabaseMigrationSchemaHintColumn{
			NotNullable:      false,
			HasServerDefault: true,
		}, 1, 0,
	},
	{
		dba.DatabaseMigrationSchemaHintColumn{
			NotNullable:      true,
			HasServerDefault: true,
		}, 1, 0,
	},
	{
		dba.DatabaseMigrationSchemaHintColumn{
			NotNullable:      false,
			HasServerDefault: false,
		}, 2_000_000, 1,
	},
	{
		dba.DatabaseMigrationSchemaHintColumn{
			NotNullable:      true,
			HasServerDefault: false,
		}, 2_000_000, 2,
	},
	{
		dba.DatabaseMigrationSchemaHintColumn{
			NotNullable:      false,
			HasServerDefault: true,
		}, 2_000_000, 1,
	},
	{
		dba.DatabaseMigrationSchemaHintColumn{
			NotNullable:      true,
			HasServerDefault: true,
		}, 2_000_000, 1,
	},
}

func TestAddColumnsToTable(t *testing.T) {
	assert := assert.New(t)

	for _, addColumnTest := range addColumnTests {
		addColumnTest.hint.Name = fakeColumn
		hints := []dba.DatabaseMigrationSchemaHint{
			{
				TableReference: dba.TableReference{
					TableName: string(fakeTableName),
				},
				Operation: "addColumn",
				Columns: []dba.DatabaseMigrationSchemaHintColumn{
					addColumnTest.hint,
				},
			},
		}

		fakeDB := &dbadminfakes.FakeDbAdmin{}
		fakeDB.GetTableSizeEstimatesReturns(map[dbadmin.TableName]uint64{
			fakeTableName: addColumnTest.tableSize,
		}, nil)

		hintsEngine := NewHintsEngine(fakeDB, largeTableDefinition)

		schemaErrors, _ := hintsEngine.ProcessHints(hints)
		assert.Equalf(
			addColumnTest.numExpectedErrors,
			len(schemaErrors),
			"Unexpected number of errors with %v, errors %v",
			addColumnTest,
			schemaErrors,
		)

		assert.Equal(1, fakeDB.GetTableSizeEstimatesCallCount())
	}
}

var addIndexTests = []struct {
	indexType             string
	addConstraintWillFail bool
	numExpectedErrors     int
}{
	{"unique", true, 1},
	{"unique", false, 0},
}

func TestAddIndexToTable(t *testing.T) {
	assert := assert.New(t)

	for _, addIndexTest := range addIndexTests {
		hints := []dba.DatabaseMigrationSchemaHint{
			{
				TableReference: dba.TableReference{
					TableName: string(fakeTableName),
				},
				Operation: "createIndex",
				Columns: []dba.DatabaseMigrationSchemaHintColumn{
					dba.DatabaseMigrationSchemaHintColumn{
						Name: fakeColumn,
					},
				},
				IndexType: addIndexTest.indexType,
			},
		}

		fakeDB := &dbadminfakes.FakeDbAdmin{}
		fakeDB.ConstraintWillFailReturns(addIndexTest.addConstraintWillFail, nil)

		hintsEngine := NewHintsEngine(fakeDB, largeTableDefinition)

		schemaErrors, _ := hintsEngine.ProcessHints(hints)
		assert.Equalf(
			addIndexTest.numExpectedErrors,
			len(schemaErrors),
			"Unexpected number of errors with %v, errors %v",
			addIndexTest,
			schemaErrors,
		)

		assert.Equal(1, fakeDB.ConstraintWillFailCallCount())
	}
}

var alterColumnTests = []struct {
	columnHint            dba.DatabaseMigrationSchemaHintColumn
	addConstraintWillFail bool
	numExpectedErrors     int
}{
	{
		dba.DatabaseMigrationSchemaHintColumn{
			NotNullable: false,
		}, false, 0,
	},
	{
		dba.DatabaseMigrationSchemaHintColumn{
			NotNullable: true,
		}, false, 0,
	},
	{
		dba.DatabaseMigrationSchemaHintColumn{
			NotNullable: true,
		}, true, 1,
	},
}

func TestAlterColumn(t *testing.T) {
	assert := assert.New(t)

	for _, alterColumnTest := range alterColumnTests {
		alterColumnTest.columnHint.Name = fakeColumn
		hints := []dba.DatabaseMigrationSchemaHint{
			{
				TableReference: dba.TableReference{
					TableName: string(fakeTableName),
				},
				Operation: "alterColumn",
				Columns: []dba.DatabaseMigrationSchemaHintColumn{
					alterColumnTest.columnHint,
				},
			},
		}

		fakeDB := &dbadminfakes.FakeDbAdmin{}

		expectedConstraintWillFailCallCount := 0
		if alterColumnTest.columnHint.NotNullable {
			fakeDB.ConstraintWillFailReturns(alterColumnTest.addConstraintWillFail, nil)
			expectedConstraintWillFailCallCount++
		}

		hintsEngine := NewHintsEngine(fakeDB, largeTableDefinition)

		schemaErrors, _ := hintsEngine.ProcessHints(hints)
		assert.Equalf(
			alterColumnTest.numExpectedErrors,
			len(schemaErrors),
			"Unexpected number of errors with %v, errors %v",
			alterColumnTest,
			schemaErrors,
		)

		assert.Equal(expectedConstraintWillFailCallCount, fakeDB.ConstraintWillFailCallCount())
	}
}
