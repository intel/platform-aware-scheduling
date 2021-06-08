package gpuscheduler

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type clientAPI struct{}

func (r *clientAPI) InClusterConfig() (*rest.Config, error) {
	return rest.InClusterConfig()
}

func (r *clientAPI) NewForConfig(config *rest.Config) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(config)
}

func (r *clientAPI) UpdatePod(clientset kubernetes.Interface, pod *v1.Pod) (*v1.Pod, error) {
	return clientset.CoreV1().Pods(pod.Namespace).Update(context.TODO(), pod, metav1.UpdateOptions{})
}

func (r *clientAPI) GetPod(clientset kubernetes.Interface, ns, name string) (*v1.Pod, error) {
	return clientset.CoreV1().Pods(ns).Get(context.TODO(), name, metav1.GetOptions{})
}
