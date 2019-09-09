package xerrors

import (
	"fmt"
)

// EnhancedError contains additional information about whether or not the error
// should be considered temporary and the original operation therefore a candidate
// for retrying.
type EnhancedError interface {
	error

	// Returns true if the error should be considered temporary or transient
	Temporary() bool
}

type temporaryError struct {
	message string
}

// NewTempErrorf will create a new base error that is always considered
// temporary and follows the calling convention of Sprintf.
func NewTempErrorf(format string, arguments ...interface{}) error {
	return temporaryError{message: fmt.Sprintf(format, arguments...)}
}

func (te temporaryError) Error() string {
	return te.message
}

func (te temporaryError) Temporary() bool {
	return true
}
