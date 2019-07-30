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

	"github.com/go-logr/logr"
	"github.com/prometheus/common/log"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dba "github.com/app-sre/dba-operator/api/v1alpha1"
	"github.com/app-sre/dba-operator/pkg/dbadmin"
	"github.com/app-sre/dba-operator/pkg/dbadmin/alembic"
	"github.com/app-sre/dba-operator/pkg/dbadmin/mysqladmin"
)

// ManagedDatabaseReconciler reconciles a ManagedDatabase object
type ManagedDatabaseReconciler struct {
	client.Client
	Log logr.Logger
}

// +kubebuilder:rbac:groups=dbaoperator.app-sre.redhat.com,resources=manageddatabases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dbaoperator.app-sre.redhat.com,resources=manageddatabases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dbaoperator.app-sre.redhat.com,resources=secrets,verbs=get

func (r *ManagedDatabaseReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	var ctx = context.Background()
	var log = r.Log.WithValues("manageddatabase", req.NamespacedName)

	log.Info("Reconcile called")

	var db dba.ManagedDatabase
	if err := r.Get(ctx, req.NamespacedName, &db); err != nil {
		log.Error(err, "unable to fetch ManagedDatabase")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, ignoreNotFound(err)
	}

	admin, err := r.initializeAdminConnection(ctx, req.Namespace, db.Spec.Connection)
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

	return ctrl.Result{}, nil
}

func (r *ManagedDatabaseReconciler) initializeAdminConnection(ctx context.Context, namespace string, conn dba.DatabaseConnectionInfo) (dbadmin.DbAdmin, error) {

	var secretName = types.NamespacedName{Namespace: namespace, Name: conn.CredentialsSecret}

	var credsSecret corev1.Secret
	if err := r.Get(ctx, secretName, &credsSecret); err != nil {
		log.Error(err, "unable to fetch credentials secret")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return nil, err
	}

	username := string(credsSecret.Data["username"])
	password := string(credsSecret.Data["password"])

	switch conn.Engine {
	case "mysql":
		return mysqladmin.CreateMySQLAdmin(
			username,
			password,
			conn.Host,
			conn.Port,
			conn.Database,
			alembic.CreateAlembicMigrationMetadata(),
		)
	}
	return nil, fmt.Errorf("Unknown database engine: %s", conn.Engine)
}

func (r *ManagedDatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dba.ManagedDatabase{}).
		Complete(r)

}
func ignoreNotFound(err error) error {
	if apierrs.IsNotFound(err) {
		return nil
	}
	return err
}
