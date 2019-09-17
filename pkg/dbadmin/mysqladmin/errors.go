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

	1020: nil, // ER_CHECKREAD
	1036: nil, // ER_OPEN_AS_READONLY
	1040: nil, // ER_CON_COUNT_ERROR
	1043: nil, // ER_HANDSHAKE_ERROR
	1053: nil, // ER_SERVER_SHUTDOWN
	1105: nil, // ER_UNKNOWN_ERROR
	1129: nil, // ER_HOST_IS_BLOCKED
	1180: nil, // ER_ERROR_DURING_COMMIT
	1181: nil, // ER_ERROR_DURING_ROLLBACK
	1182: nil, // ER_ERROR_DURING_FLUSH_LOGS
	1188: nil, // ER_MASTER
	1202: nil, // ER_SLAVE_THREAD
	1203: nil, // ER_TOO_MANY_USER_CONNECTIONS
	1205: nil, // ER_LOCK_WAIT_TIMEOUT
	1206: nil, // ER_LOCK_TABLE_FULL
	1218: nil, // ER_CONNECT_TO_MASTER
	1220: nil, // ER_ERROR_WHEN_EXECUTING_COMMAND
	1290: nil, // ER_OPTION_PREVENTS_STATEMENT
	1297: nil, // ER_GET_TEMPORARY_ERRMSG
	1317: nil, // ER_QUERY_INTERRUPTED
	1637: nil, // ER_TOO_MANY_CONCURRENT_TRXS
	1836: nil, // ER_READ_ONLY_MODE
	1874: nil, // ER_INNODB_READ_ONLY
	3019: nil, // ER_INNODB_UNDO_LOG_FULL
	3032: nil, // ER_SERVER_OFFLINE_MODE
	3168: nil, // ER_SERVER_ISNT_AVAILABLE
	3169: nil, // ER_SESSION_WAS_KILLED
	3186: nil, // ER_CAPACITY_EXCEEDED_IN_PARSER
	3572: nil, // ER_LOCK_NOWAIT

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
