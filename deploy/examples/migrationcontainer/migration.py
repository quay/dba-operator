import os
import sys
import logging
import time
import argparse
import re

import pymysql.cursors
import pymysql.err

from prometheus_client import CollectorRegistry, Gauge, Counter, push_to_gateway


FORMAT = '%(asctime)s [%(process)d] [%(levelname)s] [%(name)s] %(message)s'

TABLE_DEF = """
CREATE TABLE `alembic_version` (
    `version_num` varchar(255) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=UTF8MB4 COLLATE=utf8mb4_bin;
"""
PROM_LABEL_PREFIX = 'DBA_OP_LABEL_'

logger = logging.getLogger(__name__)

def run(db_connection_string, push_gateway_addr, job_id, labels, write_version,
        run_seconds, fail_seconds):

    logger.debug('Starting migration')
    registry = CollectorRegistry()

    migration_completion_percent = Gauge(
        'migration_completion_percent',
        'Estimate of the completion percentage of the job',
        registry=registry,
    )
    migration_complete_total = Counter(
        'migration_complete_total',
        'Binary value of whether or not the job is complete',
        registry=registry,
    )
    migration_failed_total = Counter(
        'migration_failed_total',
        'Binary value of whether or not the job has failed',
        registry=registry,
    )
    migration_items_completed_total = Counter(
        'migration_items_completed_total',
        'Number of items this migration has completed',
        registry=registry,
    )

    def update_metrics():
        push_to_gateway(push_gateway_addr, job=job_id, registry=registry,
                        grouping_key=labels)

    for i in range(run_seconds):
        if i >= fail_seconds:
            migration_failed_total.inc()
            update_metrics()
            sys.exit(1)

        migration_items_completed_total.inc()
        migration_completion_percent.set(float(i)/run_seconds)
        update_metrics()
        logger.debug('%s/%s items completed', i, run_seconds)
        time.sleep(1)

    # Write the completion to the database
    _write_database_version(db_connection_string, write_version)
    migration_complete_total.inc()
    migration_completion_percent.set(1.0)
    update_metrics()


def _parse_mysql_dsn(db_connection_string):
    # DO NOT use this regex as authoritative for a MySQL DSN
    matcher = re.match(
        r'([^:]+):([^@]+)@tcp\(([^:]+):([0-9]+)\)\/([a-zA-Z0-9]+)',
        db_connection_string,
    )
    assert matcher is not None

    return {
        "host": matcher.group(3),
        "user": matcher.group(1),
        "password": matcher.group(2),
        "database": matcher.group(5),
        "port": int(matcher.group(4)),
    }


def _write_database_version(db_connection_string, version):
    connection_params = _parse_mysql_dsn(db_connection_string)
    db_conn = pymysql.connect(autocommit=True, **connection_params)

    try:
        with db_conn.cursor() as cursor:
            sql = "UPDATE alembic_version SET version_num = %s"
            cursor.execute(sql, (version))
    except pymysql.err.ProgrammingError:
        # Likely the table was missing
        with db_conn.cursor() as cursor:
            cursor.execute(TABLE_DEF)
            create = "INSERT INTO alembic_version (version_num) VALUES (%s)"
            cursor.execute(create, (version))


def _process_label_key(label_key):
    return label_key[len(PROM_LABEL_PREFIX):].lower()


if __name__ == '__main__':
    logging.basicConfig(format=FORMAT, level=logging.DEBUG)

    check_vars = [
        'DBA_OP_PROMETHEUS_PUSH_GATEWAY_ADDR',
        'DBA_OP_JOB_ID',
        'DBA_OP_CONNECTION_STRING',
    ]
    for env_var_name in check_vars:
        if not env_var_name in os.environ:
            logger.error('Must provide the environment variable %s', env_var_name)
            sys.exit(1)
    
    logger = logging.getLogger(os.environ['DBA_OP_JOB_ID'])

    parser = argparse.ArgumentParser(
        description='Run a fake migration container.',
    )
    parser.add_argument(
        '--seconds',
        default=30,
        type=int,
        help='Number of seconds for which to run',
    )
    parser.add_argument(
        '--fail_after',
        default=sys.maxsize,
        type=int,
        help='Number of seconds after which to fail (default: succeed)',
    )
    parser.add_argument(
        '--write_version',
        required=True,
        type=str,
        help='Database version to set after completion',
    )
    args = parser.parse_args()

    # Parse the env to find labels that we need to add
    labels = {_process_label_key(k): v for k, v in os.environ.items()
              if k.startswith(PROM_LABEL_PREFIX)}

    run(
        os.environ['DBA_OP_CONNECTION_STRING'],
        os.environ['DBA_OP_PROMETHEUS_PUSH_GATEWAY_ADDR'],
        os.environ['DBA_OP_JOB_ID'],
        labels,
        args.write_version,
        args.seconds,
        args.fail_after,
    )
