package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrPodReadyConditionNotFound = errors.New("pod's ready condition not found")
	ErrPodCompletedWithFailure   = errors.New("pod completed: Failed")
	ErrPodCompletedWithSuccess   = errors.New("pod completed: Succeeded")
)

func CreateNamespace(ctx context.Context, c client.Client, name string) error {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "namespace",
			Name:      name,
		},
	}

	if err := c.Create(ctx, namespace); err != nil {
		return fmt.Errorf("failed to create namespace '%s': (%w)", namespace, err)
	}

	return nil
}

func DeleteNamespace(ctx context.Context, c client.Client, name string) error {
	namespace := &corev1.Namespace{}
	if err := c.Get(ctx, client.ObjectKey{Name: name}, namespace); err != nil {
		return fmt.Errorf("failed to get namespace '%s': (%w)", namespace, err)
	}

	if err := c.Delete(ctx, namespace); err != nil {
		return fmt.Errorf("failed to delete namespace '%s': (%w)", namespace, err)
	}

	return nil
}

func MakeDeployment(pathToYaml string) (*appsv1.Deployment, error) {
	manifest, err := AbsPathToFile(pathToYaml)
	if err != nil {
		return nil, err
	}

	deployment := appsv1.Deployment{}
	if err := yaml.NewYAMLOrJSONDecoder(manifest, 100).Decode(&deployment); err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("failed to decode file %s", pathToYaml))
	}

	return &deployment, nil
}

func CreateDeployment(ctx context.Context, c client.Client, namespace string, deployment *appsv1.Deployment) error {
	deployment.Namespace = namespace
	err := c.Create(ctx, deployment)
	if err != nil {
		return fmt.Errorf("failed to create deployment %s: %w", deployment.Name, err)
	}

	return nil
}

func MakeService(pathToYaml string) (*corev1.Service, error) {
	manifest, err := AbsPathToFile(pathToYaml)
	if err != nil {
		return nil, err
	}

	service := corev1.Service{}
	if err := yaml.NewYAMLOrJSONDecoder(manifest, 100).Decode(&service); err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("failed to decode file %s", pathToYaml))
	}

	return &service, nil
}

func CreateService(ctx context.Context, c client.Client, namespace string, service *corev1.Service) error {
	service.Namespace = namespace
	err := c.Create(ctx, service)
	if err != nil {
		return fmt.Errorf("failed to create deployment %s: %w", service.Name, err)
	}

	return nil
}

func WaitForServiceReady(ctx context.Context, c client.Client, timeout time.Duration, namespace, servicename string) error {
	return wait.Poll(time.Second, timeout, func() (bool, error) {
		service := &corev1.Service{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: servicename}, service); err != nil {
			return false, fmt.Errorf("failed to get service %s: (%w)", namespace+"/"+servicename, err)
		}

		if service.Spec.Type == corev1.ServiceTypeExternalName {
			return true, nil
		}

		if service.Spec.ClusterIP != corev1.ClusterIPNone && service.Spec.ClusterIP == "" {
			return false, nil
		}

		if service.Spec.Type == corev1.ServiceTypeLoadBalancer && service.Status.LoadBalancer.Ingress == nil {
			return false, nil
		}

		return true, nil
	})
}

func WaitForPodsReady(ctx context.Context, c client.Client, timeout time.Duration, namespace string, replicas int, opts *client.ListOptions) error {
	return wait.Poll(time.Second, timeout, func() (bool, error) {
		podList := &corev1.PodList{}
		err := c.List(ctx, podList, opts)
		if err != nil {
			return false, fmt.Errorf("failed to list pods in namespace %s: %w", namespace, err)
		}

		runningAndReady := 0
		for _, pod := range podList.Items {
			isRunningAndReady, err := PodRunningAndReady(pod)
			if err != nil {
				return false, err
			}

			if isRunningAndReady {
				runningAndReady++
			}
		}

		if replicas == runningAndReady {
			return true, nil
		}

		return false, nil
	})
}

func PodRunningAndReady(pod corev1.Pod) (bool, error) {
	switch pod.Status.Phase {
	case corev1.PodFailed:
		return false, fmt.Errorf("pod completed: (failed): %s", pod.Name+pod.Namespace)
	case corev1.PodSucceeded:
		return false, fmt.Errorf("pod completed: (failed): %s", pod.Name+pod.Namespace)
	case corev1.PodRunning:
		for _, cond := range pod.Status.Conditions {
			if cond.Type != corev1.PodReady {
				continue
			}
			return cond.Status == corev1.ConditionTrue, nil
		}
		return false, fmt.Errorf("pod's ready condition not found: %s", pod.Name+pod.Namespace)
	}
	return false, nil
}

func AbsPathToFile(relativPath string) (*os.File, error) {
	path, err := filepath.Abs(relativPath)
	if err != nil {
		return nil, fmt.Errorf("failed generate absolute file path of %s: (%w)", relativPath, err)
	}

	manifest, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: (%w)", path, err)
	}

	return manifest, nil
}
