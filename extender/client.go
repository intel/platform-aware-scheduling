package extender

import (
	"log"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// GetKubeClient returns the kube client interface with its config.
func GetKubeClient(kubeConfig string) (kubernetes.Interface, *rest.Config, error) {
	clientConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Print("not in cluster - trying file-based configuration")
		clientConfig, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
		if err != nil {
			return nil, nil, err
		}
	}
	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, nil, err
	}
	return kubeClient, clientConfig, nil
}
