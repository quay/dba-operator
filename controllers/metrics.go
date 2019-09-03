package controllers

import (
	"reflect"

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
