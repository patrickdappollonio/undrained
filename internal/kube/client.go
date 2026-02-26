package kube

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// NewClient builds a Kubernetes client using the following precedence:
//  1. Explicit kubeconfig path (if provided)
//  2. KUBECONFIG env var / default ~/.kube/config
//  3. In-cluster service account
func NewClient(kubeconfig, kubeContext string) (kubernetes.Interface, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}

	configOverrides := &clientcmd.ConfigOverrides{}
	if kubeContext != "" {
		configOverrides.CurrentContext = kubeContext
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		configOverrides,
	).ClientConfig()

	if err != nil {
		// Fall back to in-cluster config.
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("unable to build kubernetes config (tried kubeconfig and in-cluster): %w", err)
		}
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	return client, nil
}
