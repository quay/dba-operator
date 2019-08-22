/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	batchv1 "k8s.io/api/batch/v1"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dba "github.com/app-sre/dba-operator/api/v1alpha1"
	"github.com/app-sre/dba-operator/internal/vergraph"
	"github.com/app-sre/dba-operator/pkg/dbadmin"
	"github.com/app-sre/dba-operator/pkg/dbadmin/alembic"
	"github.com/app-sre/dba-operator/pkg/dbadmin/mysqladmin"
)

// ManagedDatabaseReconciler reconciles a ManagedDatabase object
type ManagedDatabaseController struct {
	client.Client
	Log            logr.Logger
	migrationGraph *vergraph.VersionGraph
	Scheme         *runtime.Scheme
	metrics        ControllerMetrics
	databaseLinks  map[string]interface{}
}

type ControllerMetrics struct {
	MigrationJobsSpawned prometheus.Counter
	CredentialsCreated   prometheus.Counter
	CredentialsRevoked   prometheus.Counter
	RegisteredMigrations prometheus.Gauge
	ManagedDatabases     prometheus.Gauge
}

func getAllMetrics(metrics ControllerMetrics) []prometheus.Collector {
	metricsValue := reflect.ValueOf(metrics)
	collectors := make([]prometheus.Collector, 0, metricsValue.NumField())
	for i := 0; i < metricsValue.NumField(); i++ {
		collectors = append(collectors, metricsValue.Field(i).Interface().(prometheus.Collector))
	}
	return collectors
}

func NewManagedDatabaseController(c client.Client, scheme *runtime.Scheme, l logr.Logger) (*ManagedDatabaseController, []prometheus.Collector) {
	metrics := ControllerMetrics{
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

	return &ManagedDatabaseController{
		Client:         c,
		Scheme:         scheme,
		Log:            l,
		metrics:        metrics,
		migrationGraph: vergraph.NewVersionGraph(),
		databaseLinks:  make(map[string]interface{}),
	}, getAllMetrics(metrics)
}

type ManagedDatabaseReconciler struct {
	controller *ManagedDatabaseController
}

type DatabasemMigrationReconciler struct {
	controller *ManagedDatabaseController
}

// +kubebuilder:rbac:groups=dbaoperator.app-sre.redhat.com,resources=manageddatabases;databasemigrations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dbaoperator.app-sre.redhat.com,resources=manageddatabases/status;databasemigrations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dbaoperator.app-sre.redhat.com,resources=secrets,verbs=get

func (c *ManagedDatabaseController) ReconcileManagedDatabase(req ctrl.Request) (ctrl.Result, error) {
	var ctx = context.Background()
	var log = c.Log.WithValues("manageddatabase", req.NamespacedName)

	var db dba.ManagedDatabase
	if err := c.Get(ctx, req.NamespacedName, &db); err != nil {
		log.Error(err, "unable to fetch ManagedDatabase")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, ignoreNotFound(err)
	}

	// TODO: handle the delete case
	c.databaseLinks[db.SelfLink] = nil
	c.metrics.ManagedDatabases.Set(float64(len(c.databaseLinks)))

	admin, err := c.initializeAdminConnection(ctx, req.Namespace, db.Spec.Connection)
	if err != nil {
		log.Error(err, "unable to create database connection")

		return ctrl.Result{}, err
	}

	version, err := admin.GetSchemaVersion()
	if err != nil {
		log.Error(err, "unable to retrieve database version")
		return ctrl.Result{}, err
	}
	log.Info("Versions", "startVersion", version, "desiredVersion", db.Spec.DesiredSchemaVersion)

	db.Status.CurrentVersion = version

	needVersion := db.Spec.DesiredSchemaVersion
	var migrationToRun *dba.DatabaseMigration

	for needVersion != version {
		// Let's run a migration
		found := c.migrationGraph.Find(needVersion)
		if found == nil {
			notFound := fmt.Errorf("Unable to find required migration: %s", needVersion)
			log.Error(notFound, "Missing migration", "missingVersion", needVersion)
			db.Status.Errors = append(db.Status.Errors, notFound.Error())
			return ctrl.Result{Requeue: true, RequeueAfter: 60 * time.Second}, notFound
		}

		migrationToRun = found.Version
		needVersion = found.Version.Spec.Previous
	}

	if migrationToRun != nil {
		log := log.WithValues("migration", migrationToRun.Name)

		// Check if this migration is already running
		labelSelector := make(map[string]string)
		labelSelector["migration-uid"] = string(migrationToRun.UID)
		labelSelector["database-uid"] = string(db.UID)

		var childJobs batchv1.JobList
		if err := c.List(ctx, &childJobs, client.InNamespace(req.Namespace), client.MatchingLabels(labelSelector)); err != nil {
			log.Error(err, "unable to list migration Jobs")
			return ctrl.Result{}, err
		}

		// We're not running a migration
		if len(childJobs.Items) == 0 {
			if migrationToRun.Spec.Previous != "" {
				// Deprovision the user account from two migrations ago, if there was one
				currentVersion := c.migrationGraph.Find(migrationToRun.Spec.Previous)
				if currentVersion == nil {
					notFound := fmt.Errorf("Unable to find current migration: %s", version)
					log.Error(notFound, "Missing migration", "missingVersion", version)
					db.Status.Errors = append(db.Status.Errors, notFound.Error())
					return ctrl.Result{Requeue: true, RequeueAfter: 60 * time.Second}, notFound
				}

				if currentVersion.Version.Spec.Previous != "" {
					prevUsername := migrationDBUsername(currentVersion.Version.Spec.Previous)
					log.Info("Deprovisioning user account", "username", prevUsername)

					// TODO: can we find out from k8s if the secret is still being used
					// Delete the secret that the application may be using
					prevSecretName := migrationName(db.Name, currentVersion.Version.Spec.Previous)
					if err := deleteSecret(ctx, c.Client, req.Namespace, prevSecretName); err != nil {
						return ctrl.Result{}, err
					}

					// Delete the user acccount
					if err := admin.VerifyUnusedAndDeleteCredentials(prevUsername); err != nil {
						return ctrl.Result{}, err
					}

					c.metrics.CredentialsRevoked.Inc()
				} else {
					log.Info("No prior migration that requires cleanup")
				}
			} else {
				log.Info("No prior migration that requires cleanup")
			}

			// Provision the user account
			newUsername := migrationDBUsername(migrationToRun.Name)
			newPassword, err := randPassword()
			if err != nil {
				return ctrl.Result{}, err
			}

			log.Info("Provisioning user account", "username", newUsername)
			if err := admin.WriteCredentials(newUsername, newPassword); err != nil {
				return ctrl.Result{}, err
			}

			// Write the secret that the application will use
			secretLabels := getStandardLabels(&db, migrationToRun)
			newSecretName := migrationName(db.Name, migrationToRun.Name)
			if err := writeCredentialsSecret(ctx, c.Client, req.Namespace, newSecretName, newUsername, newPassword, secretLabels); err != nil {
				return ctrl.Result{}, err
			}

			c.metrics.CredentialsCreated.Inc()

			// Start the migration
			log.Info("Running migration", "currentVersion", migrationToRun.Spec.Previous)
			job, err := c.constructJobForMigration(&db, migrationToRun, db.Spec.Connection.DSNSecret)
			if err != nil {
				return ctrl.Result{}, err
			}

			if err := c.Create(ctx, job); err != nil {
				log.Error(err, "unable to create Job for migration", "job", job)
				return ctrl.Result{}, err
			}

			c.metrics.MigrationJobsSpawned.Inc()
		} else {
			log.Info("Found matching migration")

			// Determine if the migration is done
			migrationToCheck := childJobs.Items[0]
			if migrationToCheck.Status.Succeeded > 0 {
				log.Info("Migration is complete")

				// TODO: Maybe do some cleanup here until TTLAfterFinished is ready
			}
		}
	}

	return ctrl.Result{}, nil
}

func (c *ManagedDatabaseController) constructJobForMigration(managedDatabase *dba.ManagedDatabase, migration *dba.DatabaseMigration, secretName string) (*batchv1.Job, error) {
	name := migrationName(managedDatabase.Name, migration.Name)

	var containerSpec corev1.Container
	migration.Spec.MigrationContainerSpec.DeepCopyInto(&containerSpec)

	falseBool := false
	csSource := &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
		Key:                  "dsn",
		Optional:             &falseBool,
	}}
	containerSpec.Env = append(containerSpec.Env, corev1.EnvVar{Name: "DBA_OP_CONNECTION_STRING", ValueFrom: csSource})
	containerSpec.Env = append(containerSpec.Env, corev1.EnvVar{Name: "DBA_OP_MIGRATION_ID", Value: name})
	containerSpec.Env = append(containerSpec.Env, corev1.EnvVar{Name: "DBA_OP_PROMETHEUS_PUSH_GATEWAY_ADDR", Value: "prom-pushgateway:9091"})

	containerSpec.ImagePullPolicy = "IfNotPresent" // TODO removeme before prod

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      getStandardLabels(managedDatabase, migration),
			Annotations: make(map[string]string),
			Name:        name,
			Namespace:   managedDatabase.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						containerSpec,
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}

	// TODO figure out a policy for adding annotations and labels

	if err := ctrl.SetControllerReference(managedDatabase, job, c.Scheme); err != nil {
		return nil, err
	}

	return job, nil
}

func (c *ManagedDatabaseController) ReconcileDatabaseMigration(req ctrl.Request) (ctrl.Result, error) {
	var ctx = context.Background()
	var log = c.Log.WithValues("databasemigration", req.NamespacedName)

	var db dba.DatabaseMigration
	if err := c.Get(ctx, req.NamespacedName, &db); err != nil {
		log.Error(err, "unable to fetch DatabaseMigration")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, ignoreNotFound(err)
	}

	c.migrationGraph.Add(&db)

	c.metrics.RegisteredMigrations.Set(float64(c.migrationGraph.Len()))

	log.Info("Updated migration graph", "graphLen", c.migrationGraph.Len(), "missingSources", c.migrationGraph.Missing())

	return ctrl.Result{}, nil
}

func (c *ManagedDatabaseController) initializeAdminConnection(ctx context.Context, namespace string, conn dba.DatabaseConnectionInfo) (dbadmin.DbAdmin, error) {

	secretName := types.NamespacedName{Namespace: namespace, Name: conn.DSNSecret}

	var credsSecret corev1.Secret
	if err := c.Get(ctx, secretName, &credsSecret); err != nil {
		log.Error(err, "unable to fetch credentials secret")
		return nil, err
	}

	dsn := string(credsSecret.Data["dsn"])

	switch conn.Engine {
	case "mysql":
		return mysqladmin.CreateMySQLAdmin(dsn, alembic.CreateAlembicMigrationMetadata())
	}
	return nil, fmt.Errorf("Unknown database engine: %s", conn.Engine)
}

func (c *ManagedDatabaseController) SetupWithManager(mgr ctrl.Manager) error {
	err := ctrl.NewControllerManagedBy(mgr).
		For(&dba.ManagedDatabase{}).
		Owns(&batchv1.Job{}).
		Complete(reconcile.Func(c.ReconcileManagedDatabase))
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&dba.DatabaseMigration{}).
		Complete(reconcile.Func(c.ReconcileDatabaseMigration))
}

func ignoreNotFound(err error) error {
	if apierrs.IsNotFound(err) {
		return nil
	}
	return err
}

func migrationName(dbName, migrationName string) string {
	return fmt.Sprintf("%s-%s", dbName, migrationName)
}

func migrationDBUsername(migrationName string) string {
	return fmt.Sprintf("dba_%s", migrationName)
}

func randPassword() (string, error) {
	identBytes := make([]byte, 16)
	if _, err := rand.Read(identBytes); err != nil {
		return "", err
	}

	return hex.EncodeToString(identBytes), nil
}

func getStandardLabels(db *dba.ManagedDatabase, migration *dba.DatabaseMigration) map[string]string {
	return map[string]string{
		"migration":     string(migration.Name),
		"migration-uid": string(migration.UID),
		"database":      string(db.Name),
		"database-uid":  string(db.UID),
	}
}

func deleteSecret(ctx context.Context, apiClient client.Client, namespace, secretName string) error {
	qSecretName := types.NamespacedName{Namespace: namespace, Name: secretName}
	var secret corev1.Secret
	if err := apiClient.Get(ctx, qSecretName, &secret); err != nil {
		log.Error(err, "unable to fetch credentials secret")
		return err
	}
	return apiClient.Delete(ctx, &secret)
}

func writeCredentialsSecret(ctx context.Context, apiClient client.Client, namespace, secretName, username, password string, labels map[string]string) error {
	newSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      labels,
			Annotations: make(map[string]string),
			Name:        secretName,
			Namespace:   namespace,
		},
		StringData: map[string]string{
			"username": username,
			"password": password,
		},
	}

	return apiClient.Create(ctx, &newSecret)
}
