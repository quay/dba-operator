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
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	batchv1 "k8s.io/api/batch/v1"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
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
	mapset "github.com/deckarep/golang-set"
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

func NewManagedDatabaseController(c client.Client, scheme *runtime.Scheme, l logr.Logger) (*ManagedDatabaseController, []prometheus.Collector) {
	metrics := generateControllerMetrics()

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
// +kubebuilder:rbac:groups=,resources=secrets,verbs=get;list;create;delete

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

	admin, err := initializeAdminConnection(ctx, log, c.Client, req.Namespace, db.Spec.Connection)
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

		oneMigration := migrationContext{
			ctx:     ctx,
			log:     log.WithValues("migration", migrationToRun.Name),
			db:      &db,
			version: migrationToRun,
		}

		if err := c.reconcileCredentialsForVersion(oneMigration, admin); err != nil {
			return ctrl.Result{}, err
		}

		if err := c.reconcileMigrationJob(oneMigration); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

type migrationContext struct {
	ctx     context.Context
	log     logr.Logger
	db      *dba.ManagedDatabase
	version *dba.DatabaseMigration
}

func (c *ManagedDatabaseController) reconcileMigrationJob(oneMigration migrationContext) error {
	oneMigration.log.Info("Reconciling migration jobs")

	// Check if this migration is already running
	labelSelector := make(map[string]string)
	labelSelector["database-uid"] = string(oneMigration.db.UID)

	var jobsForDatabase batchv1.JobList
	if err := c.List(oneMigration.ctx, &jobsForDatabase, client.InNamespace(oneMigration.db.Namespace), client.MatchingLabels(labelSelector)); err != nil {
		oneMigration.log.Error(err, "unable to list migration Jobs")
		return err
	}

	foundJob := false
	for _, job := range jobsForDatabase.Items {
		if job.Labels["migration-uid"] == string(oneMigration.version.UID) {
			// This is the job for the migration in question
			oneMigration.log.Info("Found matching migration")
			foundJob = true

			if job.Status.Succeeded > 0 {
				oneMigration.log.Info("Migration is complete")

				// TODO: should we write the metric here or wait until cleanup?
			}
		} else {
			// This is an old job and should be cleaned up
			oneMigration.log.Info("Cleaning up job for old migration", "oldMigrationName", job.Name)

			if err := c.Client.Delete(oneMigration.ctx, &job); err != nil {
				return err
			}

			// TODO: maybe write metrics here?
		}
	}

	if !foundJob {
		// Start the migration
		oneMigration.log.Info("Running migration", "currentVersion", oneMigration.version.Spec.Previous)
		job, err := constructJobForMigration(oneMigration.db, oneMigration.version, oneMigration.db.Spec.Connection.DSNSecret)
		if err != nil {
			return err
		}

		// Set the CR to own the new job
		if err := ctrl.SetControllerReference(oneMigration.db, job, c.Scheme); err != nil {
			return err
		}

		if err := c.Create(oneMigration.ctx, job); err != nil {
			oneMigration.log.Error(err, "unable to create Job for migration", "job", job)
			return err
		}

		c.metrics.MigrationJobsSpawned.Inc()
	}

	return nil
}

func (c *ManagedDatabaseController) reconcileCredentialsForVersion(oneMigration migrationContext, admin dbadmin.DbAdmin) error {
	oneMigration.log.Info("Reconciling credentials")

	// Compute the list of credentials that we need for this database version
	// TODO: we only want to write the credentials for the current version if
	// the db has actually made it here
	secretNames := mapset.NewSet(migrationName(oneMigration.db.Name, oneMigration.version.Name))
	dbUsernames := mapset.NewSet(migrationDBUsername(oneMigration.version.Name))

	if oneMigration.version.Spec.Previous != "" {
		secretNames.Add(migrationName(oneMigration.db.Name, oneMigration.version.Spec.Previous))
		dbUsernames.Add(migrationDBUsername(oneMigration.version.Spec.Previous))
	}

	// List the secrets in the system
	secretList, err := listSecretsForDatabase(oneMigration.ctx, c.Client, oneMigration.db)
	if err != nil {
		return err
	}

	existingSecretSet := mapset.NewSet()
	for _, foundSecret := range secretList.Items {
		existingSecretSet.Add(foundSecret.Name)
	}

	// Remove any secrets that shouldn't be there
	secretsToRemove := existingSecretSet.Difference(secretNames)
	for secretToRemove := range secretsToRemove.Iterator().C {
		if err := deleteSecretIfUnused(oneMigration.ctx, oneMigration.log, c.Client, oneMigration.db.Namespace, secretToRemove.(string)); err != nil {
			return err
		}
	}

	// List credentials in the database that match our namespace prefix
	// TODO: extract out prefix into config or a global
	existingDbUsernames, err := admin.ListUsernames("dba_")
	if err != nil {
		return err
	}
	oneMigration.log.Info("Found matching usernames", "numUsername", len(existingDbUsernames))

	existingDbUsernamesSet := mapset.NewSet()
	for _, username := range existingDbUsernames {
		existingDbUsernamesSet.Add(username)
	}

	// Remove any users that shouldn't be there
	dbUsersToRemove := existingDbUsernamesSet.Difference(dbUsernames)
	for dbUserToRemoveItem := range dbUsersToRemove.Iterator().C {
		dbUserToRemove := dbUserToRemoveItem.(string)
		oneMigration.log.Info("Deprovisioning user account", "username", dbUserToRemove)
		if err := admin.VerifyUnusedAndDeleteCredentials(dbUserToRemove); err != nil {
			return err
		}
		c.metrics.CredentialsRevoked.Inc()
	}

	// Create any missing credentials in the database
	dbUsersToAdd := dbUsernames.Difference(existingDbUsernamesSet)
	secretsToAdd := secretNames.Difference(existingSecretSet)
	for dbUserToAddItem := range dbUsersToAdd.Iterator().C {
		dbUserToAdd := dbUserToAddItem.(string)
		newPassword, err := randPassword()
		if err != nil {
			return err
		}

		// Write the database user
		oneMigration.log.Info("Provisioning user account", "username", dbUserToAdd)
		if err := admin.WriteCredentials(dbUserToAdd, newPassword); err != nil {
			return err
		}

		// Write the corresponding secret
		secretLabels := getStandardLabels(oneMigration.db, oneMigration.version)
		newSecretName := migrationName(oneMigration.db.Name, oneMigration.version.Name)
		if err := writeCredentialsSecret(oneMigration.ctx, c.Client, oneMigration.db.Namespace, newSecretName, dbUserToAdd, newPassword, secretLabels); err != nil {
			return err
		}

		c.metrics.CredentialsCreated.Inc()

		secretsToAdd.Remove(newSecretName)
	}

	// TODO: handle the case of regenerating any database users for which we've
	// lost the secret (secretsToAdd remainder)
	return nil
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

	// TODO handle the delete and update cases

	c.migrationGraph.Add(&db)

	c.metrics.RegisteredMigrations.Set(float64(c.migrationGraph.Len()))

	log.Info("Updated migration graph", "graphLen", c.migrationGraph.Len(), "missingSources", c.migrationGraph.Missing())

	return ctrl.Result{}, nil
}

func (c *ManagedDatabaseController) SetupWithManager(mgr ctrl.Manager) error {
	err := ctrl.NewControllerManagedBy(mgr).
		For(&dba.ManagedDatabase{}).
		Owns(&batchv1.Job{}).
		Owns(&corev1.Secret{}).
		Complete(reconcile.Func(c.ReconcileManagedDatabase))
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&dba.DatabaseMigration{}).
		Complete(reconcile.Func(c.ReconcileDatabaseMigration))
}

func initializeAdminConnection(ctx context.Context, log logr.Logger, apiClient client.Client, namespace string, conn dba.DatabaseConnectionInfo) (dbadmin.DbAdmin, error) {

	secretName := types.NamespacedName{Namespace: namespace, Name: conn.DSNSecret}

	var credsSecret corev1.Secret
	if err := apiClient.Get(ctx, secretName, &credsSecret); err != nil {
		log.Error(err, "unable to fetch credentials secret")
		return nil, err
	}

	dsn := string(credsSecret.Data["dsn"])

	switch conn.Engine {
	case "mysql":
		// TODO: choose the migration engine from data in the CR (e.g. alembic)
		return mysqladmin.CreateMySQLAdmin(dsn, alembic.CreateAlembicMigrationMetadata())
	}
	return nil, fmt.Errorf("Unknown database engine: %s", conn.Engine)
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
