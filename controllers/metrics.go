package controllers

import (
	"io"
	"reflect"
	"time"

	dba "github.com/app-sre/dba-operator/api/v1alpha1"
	"github.com/app-sre/dba-operator/pkg/dbadmin"
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
)

// ManagedDatabaseControllerMetrics should contain all of the metrics exported
// by the ManagedDatabaseController
type ManagedDatabaseControllerMetrics struct {
	MigrationJobsSpawned prometheus.Counter
	CredentialsCreated   prometheus.Counter
	CredentialsRevoked   prometheus.Counter
	RegisteredMigrations prometheus.Gauge
	ManagedDatabases     prometheus.Gauge
}

func getAllMetrics(metrics ManagedDatabaseControllerMetrics) []prometheus.Collector {
	metricsValue := reflect.ValueOf(metrics)
	collectors := make([]prometheus.Collector, 0, metricsValue.NumField())
	for i := 0; i < metricsValue.NumField(); i++ {
		collectors = append(collectors, metricsValue.Field(i).Interface().(prometheus.Collector))
	}
	return collectors
}

func generateManagedDatabaseControllerMetrics() ManagedDatabaseControllerMetrics {
	return ManagedDatabaseControllerMetrics{
		MigrationJobsSpawned: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "dba_operator_migration_jobs_spawned_total",
		}),
		CredentialsCreated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "dba_operator_credentials_created_total",
		}),
		CredentialsRevoked: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "dba_operator_credentials_revoked_total",
		}),
		RegisteredMigrations: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dba_operator_registered_migrations_total",
		}),
		ManagedDatabases: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dba_operator_managed_databases_total",
		}),
	}
}

type databaseMetricsCollector struct {
	db  dbadmin.DbAdmin
	log logr.Logger

	rowEstimateMetrics map[dba.TableReference]*prometheus.Desc
	rowEstimateFilter  []dbadmin.TableName

	nextIDMetrics map[dba.TableReference]*prometheus.Desc
	nextIDFilter  []dbadmin.TableName

	sqlQueryMetrics []sqlQueryExporter
}

type sqlQueryExporter struct {
	spec   dba.SQLQueryMetric
	desc   *prometheus.Desc
	timing prometheus.Histogram
}

// CollectorCloser is a custom interface adding a Close method to
// prometheus.Collector
type CollectorCloser interface {
	prometheus.Collector
	io.Closer
}

// NewDatabaseMetricsCollector creates a database metrics collector for
// instrumenting the data in a live running database.
func NewDatabaseMetricsCollector(db dbadmin.DbAdmin, dbName string, log logr.Logger, metrics *dba.DataMetrics) CollectorCloser {
	rowEstimateMetrics := make(map[dba.TableReference]*prometheus.Desc)
	rowEstimateFilter := make([]dbadmin.TableName, 0)

	nextIDMetrics := make(map[dba.TableReference]*prometheus.Desc)
	nextIDFilter := make([]dbadmin.TableName, 0)

	sqlQueryMetrics := make([]sqlQueryExporter, 0)

	if metrics != nil {
		if metrics.TableEstimatedSize != nil {
			for _, tableRef := range *metrics.TableEstimatedSize {
				labels := prometheus.Labels{
					"database": dbName,
					"table":    tableRef.TableName,
				}

				rowEstimateMetrics[tableRef] = prometheus.NewDesc(
					"database_table_rows_estimate",
					"Estimate of the number of rows in the specified table",
					nil,
					labels,
				)
				rowEstimateFilter = append(rowEstimateFilter, dbadmin.TableName(tableRef.TableName))
			}
		}

		if metrics.TableNextID != nil {
			for _, tableRef := range *metrics.TableNextID {
				labels := prometheus.Labels{
					"database": dbName,
					"table":    tableRef.TableName,
				}

				nextIDMetrics[tableRef] = prometheus.NewDesc(
					"database_table_next_id",
					"The next id which will be assigned to a new row in the table",
					nil,
					labels,
				)
				nextIDFilter = append(nextIDFilter, dbadmin.TableName(tableRef.TableName))
			}
		}

		if metrics.SQLQuery != nil {
			for _, sqlQuery := range *metrics.SQLQuery {
				labels := prometheus.Labels{
					"database": dbName,
				}
				for k, v := range sqlQuery.PrometheusMetric.ExtraLabels {
					labels[k] = v
				}

				sqlQueryDesc := prometheus.NewDesc(
					sqlQuery.PrometheusMetric.Name,
					sqlQuery.PrometheusMetric.HelpString,
					nil,
					labels,
				)

				histogram := prometheus.NewHistogram(prometheus.HistogramOpts{
					Namespace: "database",
					Name:      "query_duration_seconds",
					Help:      "Histogram of database query durations",
					ConstLabels: prometheus.Labels{
						"database":         dbName,
						"verb":             "SELECT",
						"query_for_metric": sqlQuery.PrometheusMetric.Name,
					},
				})

				sqlQueryMetrics = append(sqlQueryMetrics, sqlQueryExporter{
					spec:   sqlQuery,
					desc:   sqlQueryDesc,
					timing: histogram,
				})
			}
		}
	}

	return &databaseMetricsCollector{
		db:                 db,
		log:                log,
		rowEstimateMetrics: rowEstimateMetrics,
		rowEstimateFilter:  rowEstimateFilter,
		nextIDMetrics:      nextIDMetrics,
		nextIDFilter:       nextIDFilter,
		sqlQueryMetrics:    sqlQueryMetrics,
	}
}

// Describe implements prometheus.Collector
func (dmc *databaseMetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range dmc.rowEstimateMetrics {
		ch <- desc
	}
	for _, desc := range dmc.nextIDMetrics {
		ch <- desc
	}
	for _, exporter := range dmc.sqlQueryMetrics {
		ch <- exporter.desc
		ch <- exporter.timing.Desc()
	}
}

// Collect implements prometheus.Collector
func (dmc *databaseMetricsCollector) Collect(ch chan<- prometheus.Metric) {
	estimates, err := dmc.db.GetTableSizeEstimates(dmc.rowEstimateFilter)
	if err != nil {
		dmc.log.Error(err, "Unable to get table size estimates for specified tables")
	} else {
		for tableRef, desc := range dmc.rowEstimateMetrics {
			estimate := estimates[dbadmin.TableName(tableRef.TableName)]
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(estimate))
		}
	}

	nextIDs, err := dmc.db.GetNextIDs(dmc.nextIDFilter)
	if err != nil {
		dmc.log.Error(err, "Unable to get next IDs for specified tables")
	} else {
		for tableRef, desc := range dmc.nextIDMetrics {
			nextID := nextIDs[dbadmin.TableName(tableRef.TableName)]
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(nextID))
		}
	}

	for _, exporter := range dmc.sqlQueryMetrics {
		var valueType prometheus.ValueType
		if exporter.spec.PrometheusMetric.ValueType == "counter" {
			valueType = prometheus.CounterValue
		} else if exporter.spec.PrometheusMetric.ValueType == "gauge" {
			valueType = prometheus.GaugeValue
		} else {
			dmc.log.Error(err, "Unknown prometheus metric type", "metricType", exporter.spec.PrometheusMetric.ValueType)
		}

		start := time.Now()
		if metricValue, err := dmc.db.SelectFloat(exporter.spec.Query); err != nil {
			dmc.log.Error(err, "Unable to load custom SQL metric from database", "metricName", exporter.spec.PrometheusMetric.Name)
		} else {
			duration := time.Since(start)
			exporter.timing.Observe(duration.Seconds())

			ch <- prometheus.MustNewConstMetric(exporter.desc, valueType, metricValue)
			ch <- exporter.timing
		}
	}
}

func (dmc *databaseMetricsCollector) Close() error {
	return dmc.db.Close()
}
