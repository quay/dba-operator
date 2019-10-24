package hints

import (
	"fmt"
	"reflect"

	dba "github.com/app-sre/dba-operator/api/v1alpha1"
	"github.com/app-sre/dba-operator/pkg/dbadmin"
	"github.com/iancoleman/strcase"
)

const (
	addColumnTableTooLarge        = "Attempting to add a column to a large table (%s)"
	addBlockingIndextableTooLarge = "Attempting to create an index (type=%s) to a large table (%s)"
	addNotNullableColumnNoDefault = "Attempting to add a NOT NULL column (%s) to an existing table (%s) without specifying a server default"
	addNotNullConstraintWillFail  = "Attempting to convert a column (%s) to NOT NULL on a table (%s) when null data is present"
	addUniqueIndexWillFail        = "Attempting to add a unique index to a table (%s) which contains non-unique data over the specified columns (%v)"
)

// Engine exposes the interface used to ask the engine for errors related to
// hints.
type Engine interface {
	ProcessHints([]dba.DatabaseMigrationSchemaHint) ([]dba.ManagedDatabaseError, error)
}

// NewHintsEngine creates an instace of the Engine bound to a specific database.
func NewHintsEngine(handle dbadmin.DbAdmin, largeTableThreshold uint64) Engine {
	he := &hintsEngine{
		handle:               handle,
		operationToProcessor: make(map[string]processorFunc),
		largeTableThreshold:  largeTableThreshold,
	}

	// Reflect over the methods on this type to find ones that are of the type
	// processorFunc, and store them in a map to make it easy to dispatch them
	// by name as we iterate over hints supplied by the user. Exported methods
	// will be renamed from CamelCase to lowerCamelCase as they are specified in
	// the hints schema.
	engineInstance := reflect.ValueOf(he)
	engineType := reflect.TypeOf(he)
	for i := 0; i < engineType.NumMethod(); i++ {
		method := engineType.Method(i)
		boundMethod := engineInstance.Method(i)
		if processor, ok := boundMethod.Interface().(func(dba.DatabaseMigrationSchemaHint, bool) ([]dba.ManagedDatabaseError, error)); ok {
			he.operationToProcessor[strcase.ToLowerCamel(method.Name)] = processorFunc(processor)
		}
	}

	return he
}

type processorFunc func(dba.DatabaseMigrationSchemaHint, bool) ([]dba.ManagedDatabaseError, error)

type hintsEngine struct {
	handle              dbadmin.DbAdmin
	largeTableThreshold uint64

	operationToProcessor map[string]processorFunc
}

func (he *hintsEngine) ProcessHints(hints []dba.DatabaseMigrationSchemaHint) ([]dba.ManagedDatabaseError, error) {
	tablesReferenced := make(map[dbadmin.TableName]struct{})

	// In one pass, just collect the table names
	for _, hint := range hints {
		tablesReferenced[dbadmin.TableName(hint.TableName)] = struct{}{}
	}

	// Load the table sizes for all of the referenced tables
	var tableNamesList []dbadmin.TableName
	for oneTableName := range tablesReferenced {
		tableNamesList = append(tableNamesList, oneTableName)
	}

	var migrationErrors []dba.ManagedDatabaseError
	estimates, err := he.handle.GetTableSizeEstimates(tableNamesList)
	if err != nil {
		return migrationErrors, fmt.Errorf("Unable to load table names: %w", err)
	}

	for _, hint := range hints {
		if processor, ok := he.operationToProcessor[hint.Operation]; !ok {
			unknownOp := newMigrationErrorF("Unknown hint operation: %s", hint.Operation)
			migrationErrors = append(migrationErrors, unknownOp)
		} else {
			tableSize := estimates[dbadmin.TableName(hint.TableName)]
			processorErrors, err := processor(hint, tableSize > he.largeTableThreshold)
			if err != nil {
				return migrationErrors, err
			}
			migrationErrors = append(migrationErrors, processorErrors...)
		}
	}

	return migrationErrors, nil
}

func (he *hintsEngine) AddColumn(hint dba.DatabaseMigrationSchemaHint, largeTable bool) (migrationErrors []dba.ManagedDatabaseError, err error) {
	tableName := dbadmin.TableName(hint.TableName)

	if largeTable {
		migrationErrors = append(migrationErrors, newMigrationErrorF(addColumnTableTooLarge, tableName))
	}

	for _, column := range hint.Columns {
		if column.NotNullable && !column.HasServerDefault {
			migrationErrors = append(migrationErrors, newMigrationErrorF(addNotNullableColumnNoDefault, column.Name, tableName))
		}
	}

	return
}

func (he *hintsEngine) CreateIndex(hint dba.DatabaseMigrationSchemaHint, largeTable bool) (migrationErrors []dba.ManagedDatabaseError, err error) {
	tableName := dbadmin.TableName(hint.TableName)

	switch hint.IndexType {
	case "unique":
		columnNames := make([]string, len(hint.Columns))
		for _, columnReference := range hint.Columns {
			columnNames = append(columnNames, columnReference.Name)
		}

		var willFail bool
		willFail, err = he.handle.ConstraintWillFail(tableName, dbadmin.UniqueConstraint, columnNames...)
		if err != nil {
			return
		}
		if willFail {
			migrationErrors = append(migrationErrors, newMigrationErrorF(addUniqueIndexWillFail, tableName, columnNames))
		}
	case "index":
		// Regular index creation always allows concurrent DML and will not block
		return migrationErrors, nil
	case "fulltext":
		if largeTable {
			migrationErrors = append(migrationErrors, newMigrationErrorF(addBlockingIndextableTooLarge, hint.IndexType, tableName))
		}
	default:
		return migrationErrors, fmt.Errorf("Unknown index type: %s", hint.IndexType)
	}

	return
}

func (he *hintsEngine) CreateTable(hint dba.DatabaseMigrationSchemaHint, largeTable bool) (migrationErrors []dba.ManagedDatabaseError, err error) {
	// Create table is always fast and easy
	return
}

func (he *hintsEngine) AlterColumn(hint dba.DatabaseMigrationSchemaHint, largeTable bool) (migrationErrors []dba.ManagedDatabaseError, err error) {
	tableName := dbadmin.TableName(hint.TableName)

	for _, column := range hint.Columns {
		if column.NotNullable {
			var willFail bool
			willFail, err = he.handle.ConstraintWillFail(tableName, dbadmin.NotNullConstraint, column.Name)
			if err != nil {
				return
			}

			if willFail {
				migrationErrors = append(migrationErrors, newMigrationErrorF(addNotNullConstraintWillFail, column.Name, tableName))
			}
		}
	}

	return
}

func newMigrationErrorF(message string, arguments ...interface{}) dba.ManagedDatabaseError {
	return dba.ManagedDatabaseError{
		Message:   fmt.Sprintf(message, arguments...),
		Temporary: false,
	}
}
