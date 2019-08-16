# Migration Container Interface

The migration container will push statistics and completion information to 
a Prometheus push gateway available specified in the environment. The migration
container should return `0` on success or non-zero on failure.

## Environment

### PROMETHEUS_PUSH_GATEWAY_ADDR

The address of the prometheus push gateway, in the form of:
`localhost:9091`

### MIGRATION_ID

A unique opaque string that is used by the operator to identify the specific
migration being monitored.

### CONNECTION_STRING

A database connection string that contains the username, password, hostname, port,
and logical database schema, e.g.:

`<engine>://<username>:<password>@<hostname>:<port>/<dbname>`

## Prometheus Metrics

The container must push the following metrics to the prometheus push gateway
specified, with the `job_id` set to the `MIGRATION_ID` as often as possible.

### completion_percent

| Prometheus Type | Numerical Type | Values    |
|-----------------|----------------|----------:|
| Gauge           | Float          | 0.0 - 1.0 |

An estimate of the percentage of the total migration work that has been completed.

### complete

| Prometheus Type | Numerical Type | Values |
|-----------------|----------------|-------:|
| Gauge           | Binary Int     | 0, 1   |

A signal on whether the job has completed (`1`) or not (`0`).

### failed

| Prometheus Type | Numerical Type | Values |
|-----------------|----------------|-------:|
| Gauge           | Binary Int     | 0, 1   |

A signal on whether the job has failed (`1`) or not (`0`).

### items_complete

| Prometheus Type | Numerical Type | Values  |
|-----------------|----------------|--------:|
| Counter         | Integer        | 0 - inf |

The number of things that have been completed. This is used as a hint about
whether progress is being made.
