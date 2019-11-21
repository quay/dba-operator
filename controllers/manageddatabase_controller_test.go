package controllers

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dba "github.com/app-sre/dba-operator/api/v1alpha1"
	"github.com/app-sre/dba-operator/pkg/dbadmin"
	"github.com/app-sre/dba-operator/pkg/dbadmin/dbadminfakes"
)

func newMigration(namespace, name, previous string) dba.DatabaseMigration {
	return dba.DatabaseMigration{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "dbaoperator.app-sre.redhat.com/v1alpha1",
			Kind:       "DatabaseMigration",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: dba.DatabaseMigrationSpec{
			Previous: previous,
			MigrationContainerSpec: corev1.Container{
				Name:    fmt.Sprintf("%s-container-name", name),
				Image:   "migrationcontainer/test",
				Command: []string{"migrate", name},
			},
		},
	}
}

func newDSNSecret(namespace string) corev1.Secret {
	return corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "connection-dsn",
			Namespace: namespace,
		},
		StringData: map[string]string{
			"dsn": "username:password@tcp(localhost:3306)/dbname",
		},
	}
}

func newManagedDatabase(namespace, desiredSchemaVersion string) dba.ManagedDatabase {
	return dba.ManagedDatabase{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "dbaoperator.app-sre.redhat.com/v1alpha1",
			Kind:       "ManagedDatabase",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "database-name",
			Namespace: namespace,
		},
		Spec: dba.ManagedDatabaseSpec{
			DesiredSchemaVersion: desiredSchemaVersion,
			Connection: dba.DatabaseConnectionInfo{
				Engine:    "mysql",
				DSNSecret: "connection-dsn",
			},
			MigrationEngine: "alembic",
		},
	}
}

func randIdentifier(randomBytes int) string {
	identBytes := make([]byte, randomBytes)
	rand.Read(identBytes)

	// Here we prepend "var" to handle an edge case where some hex (e.g. 1e2)
	// gets interpreted as scientific notation by MySQL
	return "var" + hex.EncodeToString(identBytes)
}

var _ = Describe("ManagedDatabaseController", func() {
	var controller *ManagedDatabaseController
	var mockDB *dbadminfakes.FakeDbAdmin

	var namespace string
	var v1, v2 dba.DatabaseMigration
	var dsnSecret corev1.Secret
	var db dba.ManagedDatabase
	var dbObjectName types.NamespacedName
	var secretsToSave []corev1.Secret

	BeforeEach(func() {
		namespace = randIdentifier(16)
		v1 = newMigration(namespace, "v1", "")
		v2 = newMigration(namespace, "v2", "v1")
		dsnSecret = newDSNSecret(namespace)
		db = newManagedDatabase(namespace, "v1")
		dbObjectName = types.NamespacedName{
			Namespace: db.Namespace,
			Name:      db.Name,
		}
		secretsToSave = nil

		controller = NewManagedDatabaseController(k8sClient, scheme.Scheme, testLogger, metrics.Registry)

		mockDB = &dbadminfakes.FakeDbAdmin{}
		controller.initializeAdminConnection = func(_, _, _ string) (dbadmin.DbAdmin, error) {
			return mockDB, nil
		}
	})

	Describe("Running Reconcile()", func() {
		var result reconcile.Result
		var err error

		AssertReconciliationSuccess := func() {
			It("should not return an error", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())
			})
			It("should not write any errors to the status block", func() {
				var afterReconcile dba.ManagedDatabase
				Expect(k8sClient.Get(context.Background(), dbObjectName, &afterReconcile)).NotTo(HaveOccurred())
				Expect(afterReconcile.Status.Errors).To(BeNil())
			})
			It("should ask the database for the current state", func() {
				Expect(mockDB.ListUsernamesCallCount()).To(Equal(1))
				Expect(mockDB.GetSchemaVersionCallCount()).To(Equal(1))
			})
		}

		AssertReconciliationError := func(numErrors int) {
			It("should not return an error", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())
			})
			It("should write an error to the status block", func() {
				var afterReconcile dba.ManagedDatabase
				Expect(k8sClient.Get(context.Background(), dbObjectName, &afterReconcile)).NotTo(HaveOccurred())
				Expect(afterReconcile.Status.Errors).To(HaveLen(numErrors))
			})
			It("should have errors that converge", func() {
				req := reconcile.Request{NamespacedName: dbObjectName}
				_, err := controller.ReconcileManagedDatabase(req)
				Expect(err).ToNot(HaveOccurred())

				var afterReconcile dba.ManagedDatabase
				Expect(k8sClient.Get(context.Background(), dbObjectName, &afterReconcile)).NotTo(HaveOccurred())
				Expect(afterReconcile.Status.Errors).To(HaveLen(numErrors))
			})
		}

		AssertJobProvisioned := func(jobContainerName string, numTotalJobs int) {
			It(fmt.Sprintf("provisions a job with the container named %s", jobContainerName), func() {
				var jobsForDatabase batchv1.JobList
				Expect(k8sClient.List(context.Background(), &jobsForDatabase, client.InNamespace(namespace))).NotTo(HaveOccurred())
				Expect(jobsForDatabase.Items).To(HaveLen(numTotalJobs))

				containerNames := make(map[string]struct{}, len(jobsForDatabase.Items))
				for _, job := range jobsForDatabase.Items {
					containerNames[job.Spec.Template.Spec.Containers[0].Name] = struct{}{}
				}
				Expect(containerNames).To(HaveKey(jobContainerName))
			})
		}

		JustBeforeEach(func() {
			ctx := context.Background()
			Expect(k8sClient.Create(ctx, &v1)).NotTo(HaveOccurred())
			Expect(k8sClient.Create(ctx, &v2)).NotTo(HaveOccurred())
			Expect(k8sClient.Create(ctx, &dsnSecret)).NotTo(HaveOccurred())
			Expect(k8sClient.Create(ctx, &db)).NotTo(HaveOccurred())

			for _, secret := range secretsToSave {
				Expect(db.UID).NotTo(Equal(""))
				secret.Labels = map[string]string{
					"database-uid": string(db.UID),
				}

				Expect(k8sClient.Create(ctx, &secret)).NotTo(HaveOccurred())
			}

			req := reconcile.Request{NamespacedName: dbObjectName}
			result, err = controller.ReconcileManagedDatabase(req)
		})

		Context("on an unmanaged database", func() {
			AssertReconciliationSuccess()

			var createdJob batchv1.Job
			Context("will create a job", func() {
				JustBeforeEach(func() {
					var jobsForDatabase batchv1.JobList
					Expect(k8sClient.List(context.Background(), &jobsForDatabase, client.InNamespace(namespace))).NotTo(HaveOccurred())
					Expect(jobsForDatabase.Items).To(HaveLen(1))
					createdJob = jobsForDatabase.Items[0]
				})

				It("brings the database up to the first version", func() {
					Expect(createdJob.Spec.Template.Spec.Containers[0].Name).To(Equal("v1-container-name"))
				})

				It("implements the migration container protocol", func() {
					env := createdJob.Spec.Template.Spec.Containers[0].Env
					envMap := make(map[string]corev1.EnvVar, len(env))
					for _, envVar := range env {
						envMap[envVar.Name] = envVar
					}

					Expect(envMap).Should(HaveKey("DBA_OP_CONNECTION_STRING"))
					Expect(envMap).Should(HaveKey("DBA_OP_CONNECTION_ENGINE"))
					Expect(envMap).Should(HaveKey("DBA_OP_JOB_ID"))
					Expect(envMap).Should(HaveKey("DBA_OP_PROMETHEUS_PUSH_GATEWAY_ADDR"))
					Expect(envMap).Should(HaveKey("DBA_OP_LABEL_DATABASE"))
					Expect(envMap).Should(HaveKey("DBA_OP_LABEL_MIGRATION"))
				})
			})
		})

		Context("on a database that is already at v1", func() {
			BeforeEach(func() {
				mockDB.GetSchemaVersionReturns("v1", nil)
			})

			AssertReconciliationSuccess()

			It("should write a new user to the database for v1", func() {
				Expect(mockDB.WriteCredentialsCallCount()).To(Equal(1))
			})
			It("should provision a new secret with credentials for the new database user", func() {
				var userSecret corev1.Secret
				newSecretName := types.NamespacedName{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%s", db.Name, v1.Name),
				}
				Expect(k8sClient.Get(context.Background(), newSecretName, &userSecret)).NotTo(HaveOccurred())
			})

			Context("but wants to be at v2", func() {
				BeforeEach(func() {
					db.Spec.DesiredSchemaVersion = "v2"

					v1Secret := corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("%s-%s", db.Name, v1.Name),
							Namespace: namespace,
						},
						StringData: map[string]string{},
					}
					secretsToSave = append(secretsToSave, v1Secret)

					mockDB.ListUsernamesReturns([]string{"dba_v1"}, nil)
				})

				AssertReconciliationSuccess()

				It("should not yet write a new user to the database for v2 yet", func() {
					Expect(mockDB.WriteCredentialsCallCount()).To(Equal(0))
				})

				AssertJobProvisioned("v2-container-name", 1)
			})

			Context("but wants to be at an invalid version", func() {
				BeforeEach(func() {
					db.Spec.DesiredSchemaVersion = "unknown"
				})

				AssertReconciliationError(1)

				It("should append a label to the database naming the missing migration", func() {
					expectedLabelValue := fmt.Sprintf("%s.unknown", namespace)
					var afterReconcile dba.ManagedDatabase
					Expect(k8sClient.Get(context.Background(), dbObjectName, &afterReconcile)).NotTo(HaveOccurred())
					Expect(afterReconcile.Labels).To(HaveKeyWithValue(BlockedByMigrationLabelKey, expectedLabelValue))
				})
			})
		})

		Context("on a database that is already at v2", func() {
			BeforeEach(func() {
				db.Spec.DesiredSchemaVersion = "v2"

				mockDB.GetSchemaVersionReturns("v2", nil)
				mockDB.ListUsernamesReturns([]string{"dba_v1"}, nil)

				v1Secret := corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("%s-%s", db.Name, v1.Name),
						Namespace: namespace,
					},
					StringData: map[string]string{},
				}

				secretsToSave = append(secretsToSave, v1Secret)
			})

			AssertReconciliationSuccess()

			It("creates the v2 credentials", func() {
				Expect(mockDB.WriteCredentialsCallCount()).To(Equal(1))

				v2SecretName := types.NamespacedName{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%s", db.Name, v2.Name),
				}
				var v2Secret corev1.Secret
				Expect(k8sClient.Get(context.Background(), v2SecretName, &v2Secret)).NotTo(HaveOccurred())
			})

			Context("but that wants to be at v3", func() {
				BeforeEach(func() {
					db.Spec.DesiredSchemaVersion = "v3"
					mockDB.ListUsernamesReturns([]string{"dba_v1", "dba_v2"}, nil)

					v2Secret := corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("%s-%s", db.Name, v2.Name),
							Namespace: namespace,
						},
						StringData: map[string]string{},
					}
					secretsToSave = append(secretsToSave, v2Secret)

					v3 := newMigration(namespace, "v3", "v2")
					Expect(k8sClient.Create(context.Background(), &v3)).NotTo(HaveOccurred())
				})

				AssertReconciliationSuccess()

				It("revokes the v1 credentials", func() {
					Expect(mockDB.VerifyUnusedAndDeleteCredentialsCallCount()).To(Equal(1))

					v1SecretName := types.NamespacedName{
						Namespace: namespace,
						Name:      fmt.Sprintf("%s-%s", db.Name, v1.Name),
					}
					var v1Secret corev1.Secret
					Expect(k8sClient.Get(context.Background(), v1SecretName, &v1Secret)).To(HaveOccurred())
				})

				AssertJobProvisioned("v3-container-name", 1)
			})

			Context("but that erroneously wants to roll back to v1", func() {
				BeforeEach(func() {
					db.Spec.DesiredSchemaVersion = "v1"
					mockDB.ListUsernamesReturns([]string{"dba_v1", "dba_v2"}, nil)

					v2Secret := corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("%s-%s", db.Name, v2.Name),
							Namespace: namespace,
						},
						StringData: map[string]string{},
					}
					secretsToSave = append(secretsToSave, v2Secret)
				})

				AssertReconciliationError(1)
			})
		})

		Context("but can't connect to the database", func() {
			BeforeEach(func() {
				controller.initializeAdminConnection = func(_, _, _ string) (dbadmin.DbAdmin, error) {
					return nil, fmt.Errorf("Can't connect to the database")
				}
			})

			AssertReconciliationError(1)
		})
	})
})
