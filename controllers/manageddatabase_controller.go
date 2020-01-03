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
	"github.com/app-sre/dba-operator/pkg/hints"
	"github.com/app-sre/dba-operator/pkg/xerrors"
)

// DBUsernamePrefix is the sequence that will be prepended to migration
// specific database credentials when created in a managed database
const DBUsernamePrefix = "dba_"

// BlockedByMigrationLabelKey is the key that will be used to add a label to a
// ManagedDatabase object indicating that it is blocked in some way by the named
// migration.
const BlockedByMigrationLabelKey = "blocked-by-migration"

var requeueAfterDelay = ctrl.Result{Requeue: true, RequeueAfter: 60 * time.Second}

// ManagedDatabaseController reconciles ManagedDatabase and DatabaseMigration objects
type ManagedDatabaseController struct {
	client.Client
	Log                       logr.Logger
	Scheme                    *runtime.Scheme
	metrics                   ManagedDatabaseControllerMetrics
	promRegistry              *prometheus.Registry
	databaseLinks             map[string]interface{}
	databaseMetricsCollectors map[string]CollectorCloser
	initializeAdminConnection func(string, string) (dbadmin.DbAdmin, error)
}

// NewManagedDatabaseController will instantiate a ManagedDatabaseController
// with the supplied arguments and logical defaults.
func NewManagedDatabaseController(c client.Client, scheme *runtime.Scheme, l logr.Logger, promRegistry *prometheus.Registry) *ManagedDatabaseController {
	metrics := generateManagedDatabaseControllerMetrics()

	dbInitializer := func(dsn, migrationEngineType string) (dbadmin.DbAdmin, error) {
		var migrationEngine dbadmin.MigrationEngine
		switch migrationEngineType {
		case "alembic":
			migrationEngine = alembic.CreateMigrationEngine()
		}

		return dbadmin.Open(dsn, migrationEngine)
	}

	return &ManagedDatabaseController{
		Client:                    c,
		Scheme:                    scheme,
		Log:                       l,
		metrics:                   metrics,
		promRegistry:              promRegistry,
		databaseLinks:             make(map[string]interface{}),
		databaseMetricsCollectors: make(map[string]CollectorCloser),
		initializeAdminConnection: dbInitializer,
	}
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

			if collector, ok := c.databaseMetricsCollectors[db.SelfLink]; ok {
				c.promRegistry.Unregister(collector)
				if err := collector.Close(); err != nil {
					log.Error(err, "Unable to close the metrics collector.")
				}
				delete(c.databaseMetricsCollectors, db.SelfLink)
			}

			c.metrics.ManagedDatabases.Set(float64(len(c.databaseLinks)))

			return ctrl.Result{}, nil
		}

		log.Error(err, "unable to fetch ManagedDatabase")
		return handleError(ctx, c.Client, &db, log, err)
	}

	// Normalize the object before reconciliation
	db.Status.Errors = nil
	if db.Labels == nil {
		db.Labels = make(map[string]string)
	}
	delete(db.Labels, BlockedByMigrationLabelKey)

	c.databaseLinks[db.SelfLink] = nil
	c.metrics.ManagedDatabases.Set(float64(len(c.databaseLinks)))

	dsnSecretName := types.NamespacedName{Namespace: db.Namespace, Name: db.Spec.Connection.DSNSecret}

	var credsSecret corev1.Secret
	if err := c.Get(ctx, dsnSecretName, &credsSecret); err != nil {
		log.Error(err, "unable to fetch credentials secret")
		return handleError(ctx, c.Client, &db, log, err)
	}

	dsn := string(credsSecret.Data["dsn"])

	metricsAdmin, err := c.initializeAdminConnection(dsn, db.Spec.MigrationEngine)
	if err != nil {
		log.Error(err, "unable to create database connection")

		return handleError(ctx, c.Client, &db, log, err)
	}

	if err := c.reconcileMetricsCollectors(metricsAdmin, log, &db); err != nil {
		return handleError(ctx, c.Client, &db, log, err)
	}

	admin, err := c.initializeAdminConnection(dsn, db.Spec.MigrationEngine)
	if err != nil {
		log.Error(err, "unable to create database connection")

		return handleError(ctx, c.Client, &db, log, err)
	}
	defer func() {
		if err := admin.Close(); err != nil {
			log.Error(err, "Unable to close the database admin connection.")
		}
	}()

	currentDbVersion, err := admin.GetSchemaVersion()
	if err != nil {
		log.Error(err, "unable to retrieve database version")
		return handleError(ctx, c.Client, &db, log, err)
	}
	log.Info("Versions", "startVersion", currentDbVersion, "desiredVersion", db.Spec.DesiredSchemaVersion)

	db.Status.CurrentVersion = currentDbVersion

	needVersion := db.Spec.DesiredSchemaVersion
	var migrationToRun *dba.DatabaseMigration

	for needVersion != currentDbVersion {
		if needVersion == "" {
			noPathErr := fmt.Errorf("Unable to find a path from the desired version (%s) to our current version (%s)", db.Spec.DesiredSchemaVersion, currentDbVersion)
			return handleError(ctx, c.Client, &db, log, noPathErr)
		}
		found, err := loadMigration(ctx, log, c.Client, db.Namespace, needVersion)
		if err != nil {
			return handleError(ctx, c.Client, &db, log, err)
		}

		migrationToRun = found
		needVersion = found.Spec.Previous
	}

	var currentVersionMigration *dba.DatabaseMigration
	if currentDbVersion != "" {
		// Load the migration for where the DB currently is
		currentVersionMigration, err = loadMigration(ctx, log, c.Client, db.Namespace, currentDbVersion)
		if err != nil {
			return handleError(ctx, c.Client, &db, log, err)
		}
	}

	migrationLog := log
	if migrationToRun != nil {
		migrationLog = migrationLog.WithValues("nextVersion", migrationToRun.Name)
	}
	if currentVersionMigration != nil {
		migrationLog = migrationLog.WithValues("activeVersion", currentVersionMigration.Name)
	}

	oneMigration := migrationContext{
		ctx:           ctx,
		log:           migrationLog,
		db:            &db,
		activeVersion: currentVersionMigration,
		nextVersion:   migrationToRun,
	}

	if err := c.reconcileCredentialsForVersion(oneMigration, admin, currentDbVersion); err != nil {
		return handleError(ctx, c.Client, &db, log, err)
	}

	if oneMigration.nextVersion != nil && oneMigration.db.Spec.HintsEngine != nil && oneMigration.db.Spec.HintsEngine.Enabled {
		hintsEngine := hints.NewHintsEngine(admin, oneMigration.db.Spec.HintsEngine.LargeTableRowsThreshold)
		migrationErrors, err := hintsEngine.ProcessHints(oneMigration.nextVersion.Spec.SchemaHints)
		if err != nil {
			migrationName := types.NamespacedName{
				Namespace: oneMigration.nextVersion.Namespace,
				Name:      oneMigration.db.Name,
			}
			blockedError := newMigrationErrorf(migrationName, "Migration contains an error: %w", err)
			return handleError(ctx, c.Client, &db, log, blockedError)
		}

		if len(migrationErrors) > 0 {
			log.Info("Migration would cause errors against running database, canceling", "migration", oneMigration.nextVersion.Name)
			oneMigration.nextVersion = nil

			db.Status.Errors = append(db.Status.Errors, migrationErrors...)
			if err := c.Client.Status().Update(ctx, &db); err != nil {
				log.Error(err, "Unable to update ManagedDatabase status block")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
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
	ctx           context.Context
	log           logr.Logger
	db            *dba.ManagedDatabase
	activeVersion *dba.DatabaseMigration
	nextVersion   *dba.DatabaseMigration
}

func (c *ManagedDatabaseController) reconcileMetricsCollectors(admin dbadmin.DbAdmin, log logr.Logger, db *dba.ManagedDatabase) error {
	log.Info("Reconciling metrics collectors")
	if collector, ok := c.databaseMetricsCollectors[db.SelfLink]; ok {
		c.promRegistry.Unregister(collector)
		if err := collector.Close(); err != nil {
			log.Error(err, "Unable to close the metrics collector.")
		}
		delete(c.databaseMetricsCollectors, db.SelfLink)
	}

	newCollector := NewDatabaseMetricsCollector(admin, db.Name, log, db.Spec.ExportDataMetrics)
	if err := c.promRegistry.Register(newCollector); err != nil {
		return fmt.Errorf("Unable to register database metrics collector: %w", err)
	}
	c.databaseMetricsCollectors[db.SelfLink] = newCollector

	return nil
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

	neededJobsRunning := make(map[string]*dba.DatabaseMigration)
	if oneMigration.nextVersion != nil {
		neededJobsRunning[string(oneMigration.nextVersion.UID)] = oneMigration.nextVersion
	}

	for _, job := range jobsForDatabase.Items {
		_, ok := neededJobsRunning[job.Labels["migration-uid"]]
		if ok {
			// This is the job for the migration in question
			oneMigration.log.Info("Found matching migration job")

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

		delete(neededJobsRunning, job.Labels["migration-uid"])
	}

	if oneMigration.db.Spec.ReadOnly {
		if len(neededJobsRunning) > 0 {
			return fmt.Errorf("Need to create %d migration(s) against read only database", len(neededJobsRunning))
		}
		return nil
	}

	for _, migration := range neededJobsRunning {
		// Start the migration
		oneMigration.log.Info("Running migration", "currentVersion", migration.Spec.Previous)
		job, err := constructJobForMigration(oneMigration.db, migration)
		if err != nil {
			return fmt.Errorf("Unable to create Job for migration (%s): %w", migration.Name, err)
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
		return nil, newMigrationErrorf(path, "Unable to fetch DatabaseMigration (%s): %w", path, err)
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

	if oneMigration.activeVersion != nil {
		// The credentials for the currently active version must be available
		desiredVersionNames = append(desiredVersionNames, oneMigration.activeVersion.Name)

		// If we are not going to attempt a migration, the previous version
		// credentials should also be preserved
		if oneMigration.activeVersion.Spec.Previous != "" && oneMigration.nextVersion == nil {
			desiredVersionNames = append(desiredVersionNames, oneMigration.activeVersion.Spec.Previous)
		}
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

	// Compute the mutations that need to be done to the database
	dbUsersToRemove := existingDbUsernamesSet.Difference(dbUsernames)
	dbUsersToAdd := dbUsernames.Difference(existingDbUsernamesSet)
	secretsToAdd := secretNames.Difference(existingSecretSet)

	// If we are read-only but have work to do, we should raise an error
	if oneMigration.db.Spec.ReadOnly {
		numMutationsRequired := dbUsersToAdd.Cardinality() + dbUsersToRemove.Cardinality() + secretsToAdd.Cardinality()
		if numMutationsRequired > 0 {
			return fmt.Errorf("DB user reconciliation requires %d mutations on a readonly database", numMutationsRequired)
		}
		return nil
	}

	// Remove any users that shouldn't be there
	for dbUserToRemoveItem := range dbUsersToRemove.Iterator().C {
		dbUserToRemove := dbUserToRemoveItem.(string)
		oneMigration.log.Info("Deprovisioning user account", "username", dbUserToRemove)
		if err := admin.VerifyUnusedAndDeleteCredentials(dbUserToRemove); err != nil {
			return fmt.Errorf("Unable to delete user (%s) from db: %w", dbUserToRemove, err)
		}
		c.metrics.CredentialsRevoked.Inc()
	}

	// Create any missing credentials in the database
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
		secretLabels := getStandardLabels(oneMigration.db, oneMigration.activeVersion)
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
	var log = c.Log.WithValues("databasemigration", req.NamespacedName)
	var ctx = context.Background()

	// Find any ManagedDatabase(s) that are blocked on us and remove the blocked
	// label to cause them to be requeued
	labelSelector := make(map[string]string)
	labelSelector[BlockedByMigrationLabelKey] = migrationLabelValue(req.NamespacedName)

	var managedDatabases dba.ManagedDatabaseList
	if err := c.List(ctx, &managedDatabases, client.InNamespace(req.Namespace), client.MatchingLabels(labelSelector)); err != nil {
		log.Error(err, "unable to list waiting ManagedDatabase(s)")
	}

	for _, db := range managedDatabases.Items {
		delete(db.ObjectMeta.Labels, BlockedByMigrationLabelKey)
		if err := c.Update(ctx, &db); err != nil {
			// If we encounter an error we should requeue so we can wake it up
			// the ManagedDatabase again on our next reconcile
			return requeueAfterDelay, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager should be called to finish initialization of a
// ManagedDatbaseController and bind it to the manager specified.
func (c *ManagedDatabaseController) SetupWithManager(mgr ctrl.Manager) error {
	// Register all static metrics with the registry
	for _, collector := range getAllMetrics(c.metrics) {
		if err := c.promRegistry.Register(collector); err != nil {
			return fmt.Errorf("Unable to registry metrics for operator: %w", err)
		}
	}

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

	statusError := dba.ManagedDatabaseError{Message: err.Error(), Temporary: false}

	var maybeTemporary xerrors.EnhancedError
	var maybeMigrationError migrationError
	updateWholeObject := false
	if errors.As(err, &maybeTemporary) && maybeTemporary.Temporary() {
		finalResult = requeueAfterDelay
		finalError = err
		statusError.Temporary = true
	} else if errors.As(err, &maybeMigrationError) {
		blockingMigrationName := maybeMigrationError.migrationName()
		db.ObjectMeta.Labels[BlockedByMigrationLabelKey] = migrationLabelValue(blockingMigrationName)
		updateWholeObject = true
	}

	db.Status.Errors = append(db.Status.Errors, statusError)

	if updateWholeObject {
		if err := apiClient.Update(ctx, db); err != nil {
			log.Error(err, "Unable to update ManagedDatabase")
			return ctrl.Result{}, err
		}
	}
	if err := apiClient.Status().Update(ctx, db); err != nil {
		log.Error(err, "Unable to update ManagedDatabase status block")
		return ctrl.Result{}, err
	}

	return finalResult, finalError
}

func migrationLabelValue(migrationName types.NamespacedName) string {
	return fmt.Sprintf("%s.%s", migrationName.Namespace, migrationName.Name)
}
