import os
import sys
import logging
import time

from prometheus_client import CollectorRegistry, Gauge, Counter, push_to_gateway


FORMAT = '%(asctime)s [%(process)d] [%(levelname)s] [%(name)s] %(message)s'

logger = logging.getLogger(os.environ['MIGRATION_ID'])

def run(push_gateway_addr, job_id, run_seconds):
    logger.debug('Starging migration')
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

    if not 'PROMETHEUS_PUSH_GATEWAY_ADDR' in os.environ:
        logger.error('Must provide the environment variable PROMETHEUS_PUSH_GATEWAY_ADDR')
        sys.exit(1)
    if not 'MIGRATION_ID' in os.environ:
        logger.error('Must provide the environment variable MIGRATION_ID')
        sys.exit(1)
        
    run(
        os.environ['PROMETHEUS_PUSH_GATEWAY_ADDR'],
        os.environ['MIGRATION_ID'],
        30,
    )
