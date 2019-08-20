import os
import sys
import logging
import time
import argparse

from prometheus_client import CollectorRegistry, Gauge, Counter, push_to_gateway


FORMAT = '%(asctime)s [%(process)d] [%(levelname)s] [%(name)s] %(message)s'

logger = logging.getLogger(__name__)

def run(db_connection_string, push_gateway_addr, job_id, run_seconds,
        fail_seconds):

    logger.debug('Starting migration')
    registry = CollectorRegistry()

    completion_percent = Gauge(
        'completion_percent',
        'Estimate of the completion percentage of the job',
        registry=registry,
    )
    complete = Gauge(
        'complete',
        'Binary value of whether or not the job is complete',
        registry=registry,
    )
    failed = Gauge(
        'failed',
        'Binary value of whether or not the job has failed',
        registry=registry,
    )
    items_completed = Counter(
        'items_completed',
        'Number of items this migration has completed',
        registry=registry,
    )

    failed.set(0)
    for i in range(run_seconds):
        if i >= fail_seconds:
            failed.set(1)
            push_to_gateway(push_gateway_addr, job=job_id, registry=registry)
            sys.exit(1)

        items_completed.inc(1)
        completion_percent.set(float(i)/run_seconds)
        push_to_gateway(push_gateway_addr, job=job_id, registry=registry)
        logger.debug('%s/%s items completed', i, run_seconds)
        time.sleep(1)

    complete.set(1)
    completion_percent.set(100)
    push_to_gateway(push_gateway_addr, job=job_id, registry=registry)


if __name__ == '__main__':
    logging.basicConfig(format=FORMAT, level=logging.DEBUG)

    if not 'DBA_OP_PROMETHEUS_PUSH_GATEWAY_ADDR' in os.environ:
        logger.error('Must provide the environment variable DBA_OP_PROMETHEUS_PUSH_GATEWAY_ADDR')
        sys.exit(1)
    if not 'DBA_OP_MIGRATION_ID' in os.environ:
        logger.error('Must provide the environment variable DBA_OP_MIGRATION_ID')
        sys.exit(1)
    if not 'DBA_OP_CONNECTION_STRING' in os.environ:
        logger.error('Must provide the environment variable DBA_OP_CONNECTION_STRING')
        sys.exit(1)
    
    logger = logging.getLogger(os.environ['DBA_OP_MIGRATION_ID'])

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
    args = parser.parse_args()

    run(
        os.environ['DBA_OP_CONNECTION_STRING'],
        os.environ['DBA_OP_PROMETHEUS_PUSH_GATEWAY_ADDR'],
        os.environ['DBA_OP_MIGRATION_ID'],
        args.seconds,
        args.fail_after,
    )
