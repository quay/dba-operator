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
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	batchv1 "k8s.io/api/batch/v1"

	mapset "github.com/deckarep/golang-set"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dba "github.com/app-sre/dba-operator/api/v1alpha1"
	"github.com/app-sre/dba-operator/pkg/dbadmin"
	"github.com/app-sre/dba-operator/pkg/dbadmin/alembic"
	"github.com/app-sre/dba-operator/pkg/dbadmin/mysqladmin"
	"github.com/app-sre/dba-operator/pkg/xerrors"
)

// DBUsernamePrefix is the sequence that will be prepended to migration
// specific database credentials when created in a managed database
const DBUsernamePrefix = "dba_"

var requeueAfterDelay = ctrl.Result{Requeue: true, RequeueAfter: 60 * time.Second}

// ManagedDatabaseController reconciles ManagedDatabase and DatabaseMigration objects
type ManagedDatabaseController struct {
	client.Client
	Log           logr.Logger
	Scheme        *runtime.Scheme
	metrics       ManagedDatabaseControllerMetrics
	databaseLinks map[string]interface{}
}

// NewManagedDatabaseController will instantiate a ManagedDatabaseController
// with the supplied arguments and logical defaults.
func NewManagedDatabaseController(c client.Client, scheme *runtime.Scheme, l logr.Logger) (*ManagedDatabaseController, []prometheus.Collector) {
	metrics := generateManagedDatabaseControllerMetrics()

	return &ManagedDatabaseController{
		Client:        c,
		Scheme:        scheme,
		Log:           l,
		metrics:       metrics,
		databaseLinks: make(map[string]interface{}),
	}, getAllMetrics(metrics)
}

// +kubebuilder:rbac:groups=dbaoperator.app-sre.redhat.com,resources=manageddatabases;databasemigrations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dbaoperator.app-sre.redhat.com,resources=manageddatabases/status;databasemigrations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=,resources=secrets,verbs=get;list;create;delete

// ReconcileManagedDatabase should be invoked whenever there is a change to a
// ManagedDatabase or one of the objects that are created on its behalf
func (c *ManagedDatabaseController) ReconcileManagedDatabase(req ctrl.Request) (ctrl.Result, error) {
	var ctx = context.Background()
	var log = c.Log.WithValues("manageddatabase", req.NamespacedName)

	var db dba.ManagedDatabase
	if err := c.Get(ctx, req.NamespacedName, &db); err != nil {
		if apierrs.IsNotFound(err) {
			delete(c.databaseLinks, db.SelfLink)
			c.metrics.ManagedDatabases.Set(float64(len(c.databaseLinks)))

			return ctrl.Result{}, nil
		}

		log.Error(err, "unable to fetch ManagedDatabase")
		return handleError(ctx, c.Client, &db, log, err)
	}

	c.databaseLinks[db.SelfLink] = nil
	c.metrics.ManagedDatabases.Set(float64(len(c.databaseLinks)))

	admin, err := initializeAdminConnection(ctx, log, c.Client, req.Namespace, &db.Spec)
	if err != nil {
		log.Error(err, "unable to create database connection")

		return handleError(ctx, c.Client, &db, log, err)
	}

	currentDbVersion, err := admin.GetSchemaVersion()
	if err != nil {
		log.Error(err, "unable to retrieve database version")
		return handleError(ctx, c.Client, &db, log, err)
	}
	log.Info("Versions", "startVersion", currentDbVersion, "desiredVersion", db.Spec.DesiredSchemaVersion)

	db.Status.Errors = nil
	db.Status.CurrentVersion = currentDbVersion

	needVersion := db.Spec.DesiredSchemaVersion
	var migrationToRun *dba.DatabaseMigration

	for needVersion != currentDbVersion {
		found, err := loadMigration(ctx, log, c.Client, db.Namespace, needVersion)
		if err != nil {
			return handleError(ctx, c.Client, &db, log, err)
		}

		migrationToRun = found
		needVersion = found.Spec.Previous
	}

	if migrationToRun == nil {
		// No need for a migration, reconcile with the version we have
		migrationToRun, err = loadMigration(ctx, log, c.Client, db.Namespace, currentDbVersion)
		if err != nil {
			return handleError(ctx, c.Client, &db, log, err)
		}
	}

	oneMigration := migrationContext{
		ctx:     ctx,
		log:     log.WithValues("migration", migrationToRun.Name),
		db:      &db,
		version: migrationToRun,
	}

	if err := c.reconcileCredentialsForVersion(oneMigration, admin, currentDbVersion); err != nil {
		return handleError(ctx, c.Client, &db, log, err)
	}

	if err := c.reconcileMigrationJob(oneMigration); err != nil {
		return handleError(ctx, c.Client, &db, log, err)
	}

	// Update the status block with the information that we've generated
	if err := c.Status().Update(ctx, &db); err != nil {
		log.Error(err, "Unable to update ManagedDatabase status block")
		return ctrl.Result{}, err
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
		return fmt.Errorf("Unable to list existing migration Job(s): %w", err)
	}

	foundJob := false
	for _, job := range jobsForDatabase.Items {
		if job.Labels["migration-uid"] == string(oneMigration.version.UID) {
			// This is the job for the migration in question
			oneMigration.log.Info("Found matching migration job")
			foundJob = true

			if job.Status.Succeeded > 0 {
				oneMigration.log.Info("Migration job is complete")

				// TODO: should we write the metric here or wait until cleanup?
			} else if job.Status.Active == 0 && job.Status.Conditions != nil {
				for _, condition := range job.Status.Conditions {
					if condition.Type == "Failed" && condition.Status == "True" {
						return fmt.Errorf("Migration job failed to complete (%s)", job.Name)
					}
				}
			}
		} else {
			// This is an old job and should be cleaned up
			oneMigration.log.Info("Cleaning up job for old migration", "oldMigrationName", job.Name)

			if err := c.Client.Delete(oneMigration.ctx, &job); err != nil {
				return fmt.Errorf("Unable to delete migration job (%s): %w", job.Name, err)
			}

			// TODO: maybe write metrics here?
		}
	}

	if !foundJob {
		// Start the migration
		oneMigration.log.Info("Running migration", "currentVersion", oneMigration.version.Spec.Previous)
		job, err := constructJobForMigration(oneMigration.db, oneMigration.version)
		if err != nil {
			return fmt.Errorf("Unable to create Job for migration (%s): %w", oneMigration.version.Name, err)
		}

		// Set the CR to own the new job
		if err := ctrl.SetControllerReference(oneMigration.db, job, c.Scheme); err != nil {
			return fmt.Errorf("Unable to set owner for new job (%s): %w", job.Name, err)
		}

		if err := c.Create(oneMigration.ctx, job); err != nil {
			oneMigration.log.Error(err, "unable to create Job for migration", "job", job.Name)
			return fmt.Errorf("Unable to create Job (%s) for migration: %w", job.Name, err)
		}

		c.metrics.MigrationJobsSpawned.Inc()
	}

	return nil
}

func loadMigration(ctx context.Context, log logr.Logger, apiClient client.Client, namespace, versionName string) (*dba.DatabaseMigration, error) {
	path := types.NamespacedName{
		Namespace: namespace,
		Name:      versionName,
	}

	var version dba.DatabaseMigration
	if err := apiClient.Get(ctx, path, &version); err != nil {
		log.Error(err, "unable to fetch DatabaseMigration")
		return nil, fmt.Errorf("Unable to fetch DatabaseMigration (%s): %w", path, err)
	}

	return &version, nil
}

func (c *ManagedDatabaseController) reconcileCredentialsForVersion(oneMigration migrationContext, admin dbadmin.DbAdmin, currentDbVersion string) error {
	oneMigration.log.Info("Reconciling credentials")

	// Compute the list of credentials that we need for this database version
	secretNames := mapset.NewSet()
	dbUsernames := mapset.NewSet()
	secretNameForUsername := make(map[string]string)

	desiredVersionNames := make([]string, 0, 2)

	if currentDbVersion == oneMigration.version.Name {
		// We have achieved the proper version, so the credentials for that
		// version should be present/added
		desiredVersionNames = append(desiredVersionNames, oneMigration.version.Name)
	}

	if oneMigration.version.Spec.Previous != "" {
		desiredVersionNames = append(desiredVersionNames, oneMigration.version.Spec.Previous)
	}

	for _, desiredVersionName := range desiredVersionNames {
		secretName := migrationName(oneMigration.db.Name, desiredVersionName)
		dbUsername := migrationDBUsername(desiredVersionName)
		secretNames.Add(secretName)
		dbUsernames.Add(dbUsername)
		secretNameForUsername[dbUsername] = secretName
	}

	// List the secrets in the system
	secretList, err := listSecretsForDatabase(oneMigration.ctx, c.Client, oneMigration.db)
	if err != nil {
		return fmt.Errorf("Unable to list existing cluster secrets: %w", err)
	}

	existingSecretSet := mapset.NewSet()
	for _, foundSecret := range secretList.Items {
		existingSecretSet.Add(foundSecret.Name)
	}

	// Remove any secrets that shouldn't be there
	secretsToRemove := existingSecretSet.Difference(secretNames)
	for secretToRemove := range secretsToRemove.Iterator().C {
		oneMigration.log.Info("Removing unneeded secret", "secretName", secretToRemove)
		if err := deleteSecretIfUnused(oneMigration.ctx, oneMigration.log, c.Client, oneMigration.db.Namespace, secretToRemove.(string)); err != nil {
			return fmt.Errorf("Unable to delete secret: %w", err)
		}
	}

	// List credentials in the database that match our namespace prefix
	existingDbUsernames, err := admin.ListUsernames(DBUsernamePrefix)
	if err != nil {
		return fmt.Errorf("Unable to list existing db usernames: %w", err)
	}

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
			return fmt.Errorf("Unable to delete user (%s) from db: %w", dbUserToRemove, err)
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
			return fmt.Errorf("Unable to add user (%s) to db: %w", dbUserToAdd, err)
		}

		// Write the database user
		oneMigration.log.Info("Provisioning user account", "username", dbUserToAdd)
		if err := admin.WriteCredentials(dbUserToAdd, newPassword); err != nil {
			return fmt.Errorf("Unable to create new db user (%s): %w", dbUserToAdd, err)
		}

		// Write the corresponding secret
		secretLabels := getStandardLabels(oneMigration.db, oneMigration.version)
		newSecretName := secretNameForUsername[dbUserToAdd]
		oneMigration.log.Info("Provisioning secret for user account", "username", dbUserToAdd, "secretName", newSecretName)
		if err := writeCredentialsSecret(
			oneMigration.ctx,
			c.Client,
			oneMigration.db.Namespace,
			newSecretName,
			dbUserToAdd,
			newPassword,
			secretLabels,
			oneMigration.db,
			c.Scheme,
		); err != nil {
			return fmt.Errorf("Unable to write secret (%s) to cluster: %w", newSecretName, err)
		}

		c.metrics.CredentialsCreated.Inc()

		secretsToAdd.Remove(newSecretName)
	}

	// TODO: handle the case of regenerating any database users for which we've
	// lost the secret (secretsToAdd remainder)
	return nil
}

// ReconcileDatabaseMigration should be invoked whenever there is a change to a
// DatabaseMigration CR or any object owned by it.
func (c *ManagedDatabaseController) ReconcileDatabaseMigration(req ctrl.Request) (ctrl.Result, error) {
	var _ = context.Background()
	var _ = c.Log.WithValues("databasemigration", req.NamespacedName)

	// These are pure data, so nothing to do for now

	return ctrl.Result{}, nil
}

// SetupWithManager should be called to finish initialization of a
// ManagedDatbaseController and bind it to the manager specified.
func (c *ManagedDatabaseController) SetupWithManager(mgr ctrl.Manager) error {
	err := ctrl.NewControllerManagedBy(mgr).
		For(&dba.ManagedDatabase{}).
		Owns(&batchv1.Job{}).
		Owns(&corev1.Secret{}).
		Complete(reconcile.Func(c.ReconcileManagedDatabase))
	if err != nil {
		return fmt.Errorf("Unable to finish operator setup: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&dba.DatabaseMigration{}).
		Complete(reconcile.Func(c.ReconcileDatabaseMigration))
}

func initializeAdminConnection(ctx context.Context, log logr.Logger, apiClient client.Client, namespace string, dbSpec *dba.ManagedDatabaseSpec) (dbadmin.DbAdmin, error) {

	secretName := types.NamespacedName{Namespace: namespace, Name: dbSpec.Connection.DSNSecret}

	var credsSecret corev1.Secret
	if err := apiClient.Get(ctx, secretName, &credsSecret); err != nil {
		log.Error(err, "unable to fetch credentials secret")
		return nil, err
	}

	dsn := string(credsSecret.Data["dsn"])

	var migrationEngine dbadmin.MigrationEngine
	switch dbSpec.MigrationEngine {
	case "alembic":
		migrationEngine = alembic.CreateMigrationEngine()
	}

	switch dbSpec.Connection.Engine {
	case "mysql":
		return mysqladmin.CreateMySQLAdmin(dsn, migrationEngine)
	}
	return nil, fmt.Errorf("Unknown database engine: %s", dbSpec.Connection.Engine)
}

func migrationName(dbName, migrationName string) string {
	return fmt.Sprintf("%s-%s", dbName, migrationName)
}

func migrationDBUsername(migrationName string) string {
	return fmt.Sprintf("%s%s", DBUsernamePrefix, migrationName)
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

func handleError(ctx context.Context, apiClient client.Client, db *dba.ManagedDatabase, log logr.Logger, err error) (finalResult ctrl.Result, finalError error) {
	var maybeTemporary xerrors.EnhancedError

	statusError := dba.ManagedDatabaseError{Message: err.Error(), Temporary: false}

	if errors.As(err, &maybeTemporary) && maybeTemporary.Temporary() {
		finalResult = requeueAfterDelay
		finalError = err
		statusError.Temporary = true
	}

	db.Status.Errors = append(db.Status.Errors, statusError)

	if err := apiClient.Status().Update(ctx, db); err != nil {
		log.Error(err, "Unable to update ManagedDatabase status block")
		return ctrl.Result{}, err
	}

	return finalResult, finalError
}
