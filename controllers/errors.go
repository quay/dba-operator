package controllers

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
)

type migrationError interface {
	error

	migrationName() types.NamespacedName
}

type migrationErrorStruct struct {
	underlying           error
	causingMigrationName types.NamespacedName
}

func newMigrationErrorf(migrationName types.NamespacedName, format string, arguments ...interface{}) migrationError {
	return migrationErrorStruct{underlying: fmt.Errorf(format, arguments...), causingMigrationName: migrationName}
}

func (me migrationErrorStruct) Error() string {
	return me.underlying.Error()
}

func (me migrationErrorStruct) Unwrap() error {
	return me.underlying
}

func (me migrationErrorStruct) migrationName() types.NamespacedName {
	return me.causingMigrationName
}
