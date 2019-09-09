package mysqladmin

import (
	"errors"

	"github.com/go-sql-driver/mysql"

	"github.com/app-sre/dba-operator/pkg/xerrors"
)

type wrappedMySQLError struct {
	error
}

var retryableErrors = map[uint16]interface{}{
	1317: nil, // ER_QUERY_INTERRUPTED
	1290: nil, // ER_OPTION_PREVENTS_STATEMENT
	1836: nil, // ER_READ_ONLY_MODE
}

func wrap(err error) xerrors.EnhancedError {
	if err != nil {
		return wrappedMySQLError{error: err}
	}
	return nil
}

// Temporary implements the EnhancedError interface
func (err wrappedMySQLError) Temporary() bool {
	switch err.error {
	case mysql.ErrInvalidConn, mysql.ErrMalformPkt, mysql.ErrPktSync, mysql.ErrPktSyncMul, mysql.ErrBusyBuffer:
		return true
	}

	var mysqle *mysql.MySQLError
	if errors.As(err.error, &mysqle) {
		if _, ok := retryableErrors[mysqle.Number]; ok {
			return true
		}
	}

	return false
}
