package controllers

import (
	dba "github.com/app-sre/dba-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var noRetries = int32(0)

func constructJobForMigration(managedDatabase *dba.ManagedDatabase, migration *dba.DatabaseMigration) (*batchv1.Job, error) {
	name := migrationName(managedDatabase.Name, migration.Name)

	var containerSpec corev1.Container
	migration.Spec.MigrationContainerSpec.DeepCopyInto(&containerSpec)

	falseBool := false
	csSource := &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: managedDatabase.Spec.Connection.DSNSecret},
		Key:                  "dsn",
		Optional:             &falseBool,
	}}
	containerSpec.Env = append(containerSpec.Env, corev1.EnvVar{Name: "DBA_OP_CONNECTION_STRING", ValueFrom: csSource})
	containerSpec.Env = append(containerSpec.Env, corev1.EnvVar{Name: "DBA_OP_JOB_ID", Value: name})
	containerSpec.Env = append(containerSpec.Env, corev1.EnvVar{Name: "DBA_OP_PROMETHEUS_PUSH_GATEWAY_ADDR", Value: "prom-pushgateway:9091"})
	containerSpec.Env = append(containerSpec.Env, corev1.EnvVar{Name: "DBA_OP_LABEL_DATABASE", Value: managedDatabase.Name})
	containerSpec.Env = append(containerSpec.Env, corev1.EnvVar{Name: "DBA_OP_LABEL_MIGRATION", Value: migration.Name})

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
			BackoffLimit: &noRetries,
		},
	}

	// TODO figure out a policy for adding annotations and labels

	return job, nil
}
