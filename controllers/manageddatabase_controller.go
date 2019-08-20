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
	"fmt"
	"time"

	"github.com/go-logr/logr"
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
}

func NewManagedDatabaseController(c client.Client, scheme *runtime.Scheme, l logr.Logger) *ManagedDatabaseController {
	return &ManagedDatabaseController{
		Client:         c,
		Scheme:         scheme,
		Log:            l,
		migrationGraph: vergraph.NewVersionGraph(),
	}
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

		// Keep the list in forward order
		migrationToRun = found.Version
		needVersion = found.Version.Spec.Previous
	}

	if migrationToRun != nil {
		log.Info("Running migration", "currentVersion", migrationToRun.Spec.Previous, "newVersion", migrationToRun.Name)

		job, err := c.constructJobForMigration(&db, migrationToRun, db.Spec.Connection.DSNSecret)
		if err != nil {
			return ctrl.Result{}, err
		}

		if err := c.Create(ctx, job); err != nil {
			log.Error(err, "unable to create Job for migration", "job", job, "migration", migrationToRun)
			return ctrl.Result{}, err
		}
	}

	// err = admin.WriteCredentials("testusername", "testpassword")
	// if err != nil {
	// 	log.Error(err, "unable to create new database user", "username", "testusername")
	// 	return ctrl.Result{}, err
	// }

	// err = admin.VerifyUnusedAndDeleteCredentials("testusername")
	// if err != nil {
	// 	log.Error(err, "unable to delete credentials", "username", "testusername")
	// 	return ctrl.Result{}, err
	// }

	return ctrl.Result{}, nil
}

func (c *ManagedDatabaseController) constructJobForMigration(managedDatabase *dba.ManagedDatabase, migration *dba.DatabaseMigration, secretName string) (*batchv1.Job, error) {
	name := fmt.Sprintf("%s-%s", managedDatabase.Name, migration.Name)

	var containerSpec corev1.Container
	migration.Spec.MigrationContainerSpec.DeepCopyInto(&containerSpec)

	falseBool := false
	csSource := &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
		Key:                  "dsn",
		Optional:             &falseBool,
	}}
	containerSpec.Env = append(containerSpec.Env, corev1.EnvVar{Name: "CONNECTION_STRING", ValueFrom: csSource})
	containerSpec.Env = append(containerSpec.Env, corev1.EnvVar{Name: "MIGRATION_ID", Value: name})
	containerSpec.Env = append(containerSpec.Env, corev1.EnvVar{Name: "PROMETHEUS_PUSH_GATEWAY_ADDR", Value: "prom-pushgateway:9091"})

	containerSpec.ImagePullPolicy = "IfNotPresent"

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      make(map[string]string),
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
	// for k, v := range cronJob.Spec.JobTemplate.Annotations {
	// 	job.Annotations[k] = v
	// }
	// job.Annotations[scheduledTimeAnnotation] = scheduledTime.Format(time.RFC3339)
	// for k, v := range cronJob.Spec.JobTemplate.Labels {
	// 	job.Labels[k] = v
	// }
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

	log.Info("Updated migration graph", "graphLen", c.migrationGraph.Len(), "missingSources", c.migrationGraph.Missing())

	return ctrl.Result{}, nil
}

func (c *ManagedDatabaseController) initializeAdminConnection(ctx context.Context, namespace string, conn dba.DatabaseConnectionInfo) (dbadmin.DbAdmin, error) {

	var secretName = types.NamespacedName{Namespace: namespace, Name: conn.DSNSecret}

	var credsSecret corev1.Secret
	if err := c.Get(ctx, secretName, &credsSecret); err != nil {
		log.Error(err, "unable to fetch credentials secret")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
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
