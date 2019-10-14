package controllers

import (
	"context"
	"fmt"

	dba "github.com/app-sre/dba-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func secretUsedInPod(secretName string, pods *corev1.PodList) *corev1.Pod {
	for _, pod := range pods.Items {
		for _, volume := range pod.Spec.Volumes {
			if volume.Secret != nil && volume.Secret.SecretName == secretName {
				return &pod
			}
		}
		for _, container := range pod.Spec.Containers {
			for _, source := range container.EnvFrom {
				if source.SecretRef != nil && source.SecretRef.Name == secretName {
					return &pod
				}
			}
			for _, envVar := range container.Env {
				if envVar.ValueFrom != nil && envVar.ValueFrom.SecretKeyRef != nil && envVar.ValueFrom.SecretKeyRef.Name == secretName {
					return &pod
				}
			}
		}
	}
	return nil
}

func deleteSecretIfUnused(ctx context.Context, log logr.Logger, apiClient client.Client, namespace, secretName string) error {
	qSecretName := types.NamespacedName{Namespace: namespace, Name: secretName}

	// Iterate pods to see if the secret is bound anywhere
	var allPods corev1.PodList
	if err := apiClient.List(ctx, &allPods, client.InNamespace(namespace)); err != nil {
		return err
	}
	if usedBy := secretUsedInPod(secretName, &allPods); usedBy != nil {
		return fmt.Errorf("Secret %s is used in pod %s", secretName, usedBy)
	}

	var secret corev1.Secret
	if err := apiClient.Get(ctx, qSecretName, &secret); err != nil {
		log.Error(err, "unable to fetch credentials secret")
		return err
	}
	return apiClient.Delete(ctx, &secret)
}

func listSecretsForDatabase(ctx context.Context, apiClient client.Client, owningDb *dba.ManagedDatabase) (*corev1.SecretList, error) {
	var foundSecrets corev1.SecretList
	labelSelector := make(map[string]string)
	labelSelector["database-uid"] = string(owningDb.UID)
	if err := apiClient.List(ctx, &foundSecrets, client.InNamespace(owningDb.Namespace), client.MatchingLabels(labelSelector)); err != nil {
		return nil, err
	}
	return &foundSecrets, nil
}

func writeCredentialsSecret(
	ctx context.Context,
	apiClient client.Client,
	namespace string,
	secretName string,
	username string,
	password string,
	labels map[string]string,
	owner metav1.Object,
	scheme *runtime.Scheme,
) error {

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

	// TODO figure out a policy for adding annotations and labels

	_ = ctrl.SetControllerReference(owner, &newSecret, scheme)

	return apiClient.Create(ctx, &newSecret)
}
