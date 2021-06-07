package e2e

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"reflect"
	"testing"
	"time"

	"github.com/pkg/errors"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/deschedule"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/dontschedule"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/scheduleonmetric"
	api "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	tasclient "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/client/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"path/filepath"
)

/*
Metrics values are currently set with a mount file which is then read by the node exporter. This behaviour could be
changed in future to allow the setting of metrics natively inside the e2e testing code. For this first iteration a new
metric and policy will be used for each of the three e2e smoke tests being reviewed.

*/

var (
	kubeConfigPath *string
	cl             *kubernetes.Clientset
	tascl          *tasclient.Client
	cm             metrics.CustomMetricsClient
)

//init sets up the clients used for the end to end tests
func init() {
	if home := homedir.HomeDir(); home != "" {
		kubeConfigPath = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "path to your kubeconfig file")
	} else {
		kubeConfigPath = flag.String("kubeconfig", "", "require absolute path to your kubeconfig file")
	}
	config, err := clientcmd.BuildConfigFromFlags("", *kubeConfigPath)
	if err != nil {
		panic(err.Error())
	}

	// create the clientset
	cl, err = kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	cm = metrics.NewClient(config)

	tascl, err = tasclient.New(*config, "default")
	if err != nil {
		panic(err.Error())
	}
	//TODO: Replace the generic timeout with an explicit check for the custom metrics from the API Server which times out after some period
	err = waitForMetrics(120 * time.Second)

	if err != nil {
		panic(err.Error())
	}
}

var (
	prioritize1Policy = getTASPolicy("prioritize1", scheduleonmetric.StrategyType, "prioritize1_metric", "GreaterThan", 0)
	filter1Policy     = getTASPolicy("filter1", dontschedule.StrategyType, "filter1_metric", "LessThan", 20)
	filter2Policy     = getTASPolicy("filter2", dontschedule.StrategyType, "filter2_metric", "Equals", 0)
	deschedule1Policy = getTASPolicy("deschedule1", deschedule.StrategyType, "deschedule1_metric", "GreaterThan", 8)
)

// TestTASFilter will test the behaviour of a pod with a listed filter/dontschedule policy in TAS
func TestTASFilter(t *testing.T) {
	tests := map[string]struct {
		policy *api.TASPolicy
		pod    *v1.Pod
		want   string
	}{
		"Filter all but one node": {policy: filter1Policy, pod: podForPolicy(fmt.Sprintf("pod-%v", time.Now().Unix()), filter1Policy.Name), want: "kind-worker2"},
		"Filter all nodes":        {policy: filter2Policy, pod: podForPolicy(fmt.Sprintf("pod-%v", rand.String(8)), filter2Policy.Name), want: ""},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			log.Printf("Running: %v\n", name)
			//defer the running of a cleanup function to remove the policy and pod after the test case
			defer cleanup(tc.pod.Name, tc.policy.Name)

			_, err := tascl.Create(tc.policy)
			if err != nil {
				log.Print(err)
			}
			time.Sleep(time.Second * 5)
			_, err = cl.CoreV1().Pods("default").Create(context.TODO(), tc.pod, metav1.CreateOptions{})
			if err != nil {
				log.Print(err)
			}

			time.Sleep(time.Second * 5)
			p, _ := cl.CoreV1().Pods("default").Get(context.TODO(), tc.pod.Name, metav1.GetOptions{})
			log.Print(p.Name)
			if !reflect.DeepEqual(tc.want, p.Spec.NodeName) {
				t.Errorf("expected: %v, got: %v", tc.want, p.Spec.NodeName)
			}
		})
	}

}

// TestTASPrioritize will test the behaviour of a pod with a listed prioritize/scheduleonmetric policy in TAS
func TestTASPrioritize(t *testing.T) {
	tests := map[string]struct {
		policy *api.TASPolicy
		pod    *v1.Pod
		want   string
	}{
		"Prioritize to highest score node": {policy: prioritize1Policy, pod: podForPolicy(fmt.Sprintf("pod-%v", time.Now().Unix()), prioritize1Policy.Name), want: "kind-worker2"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			log.Printf("Running: %v\n", name)
			//defer the running of a cleanup function to remove the policy and pod after the test case
			defer cleanup(tc.pod.Name, tc.policy.Name)

			_, err := tascl.Create(tc.policy)
			if err != nil {
				log.Print(err)
			}
			time.Sleep(time.Second * 5)
			_, err = cl.CoreV1().Pods("default").Create(context.TODO(), tc.pod, metav1.CreateOptions{})
			if err != nil {
				log.Print(err)
			}
			time.Sleep(time.Second * 5)
			p, _ := cl.CoreV1().Pods("default").Get(context.TODO(), tc.pod.Name, metav1.GetOptions{})
			log.Print(p.Name)

			if !reflect.DeepEqual(tc.want, p.Spec.NodeName) {
				t.Errorf("expected: %v, got: %v", tc.want, p.Spec.NodeName)
			}
		})
	}

}

// TestTASDeschedule will test the behaviour of a pod with a listed deschedule policy in TAS
func TestTASDeschedule(t *testing.T) {
	tests := map[string]struct {
		policy *api.TASPolicy
		want   map[string]bool
	}{
		"Label node for deschedule": {policy: deschedule1Policy, want: map[string]bool{"kind-worker2": true}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			res := map[string]bool{}
			log.Printf("Running: %v\n", name)
			//defer the running of a cleanup function to remove the policy and pod after the test case
			defer cleanup("", tc.policy.Name)
			_, err := tascl.Create(tc.policy)
			if err != nil {
				log.Print(err)
			}
			time.Sleep(time.Second * 5)
			lbls := metav1.LabelSelector{MatchLabels: map[string]string{deschedule1Policy.Name: "violating"}}

			nodes, err := cl.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: labels.Set(lbls.MatchLabels).String()})
			if err != nil {
				log.Print(err)
			}
			for _, n := range nodes.Items {
				res[n.Name] = true
			}
			if !reflect.DeepEqual(tc.want, res) {
				//Log full node specs and TAS Pod log if the test fails
				nodes, _ = cl.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
				log.Print(tasLog())
				for _, n := range nodes.Items {
					log.Printf("%v labels: %v", n.Name, n.ObjectMeta.Labels)
				}
				t.Errorf("expected: %v, got: %v", tc.want, res)
			}
		})
	}
}

//TestAddAndDeletePolicy repeats a test to show an issue in repeatedly adding and deleting policies
func TestAddAndDeletePolicy(t *testing.T) {
	repeatTest(TestTASFilter, t, 5)
}

func podForPolicy(podName, policyName string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: "default",
			Labels:    map[string]string{"telemetry-policy": policyName},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:    "test",
					Image:   "busybox",
					Command: []string{"/bin/sh", "-c", "sleep INF"},
					Resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{"telemetry/scheduling": *resource.NewQuantity(1, resource.DecimalSI)},
					},
				},
			},
		},
	}
}

func cleanup(podName string, policyName string) {
	if podName != "" {
		err := cl.CoreV1().Pods("default").Delete(context.TODO(), podName, metav1.DeleteOptions{})
		if err != nil {
			log.Print(err.Error())
		}
	}
	err := tascl.Delete(policyName, &metav1.DeleteOptions{})
	if err != nil {
		log.Print(err.Error())
	}
}

func waitForMetrics(timeout time.Duration) error {
	t := time.Now().Add(timeout)
	var failureMessage error
	for time.Now().Before(t) {
		m, err := cm.GetNodeMetric("filter1_metric")
		if len(m) > 0 {
			log.Printf("Metrics returned after %v: %v", t.Sub(time.Now()), m)
			return nil
		}
		time.Sleep(2)
		failureMessage = err
	}
	return errors.Wrap(failureMessage, "Request for custom metrics has timed out.")
}

// tasLog returns the log of the Telemetry Aware Scheduling pod as a string
func tasLog() string {
	lbls := metav1.LabelSelector{MatchLabels: map[string]string{"app": "tas"}}

	pods, err := cl.CoreV1().Pods("default").List(context.TODO(), metav1.ListOptions{LabelSelector: labels.Set(lbls.MatchLabels).String()})
	if err != nil {
		return "error in getting config"
	}
	if len(pods.Items) <= 0 {
		return "Tas logs not found in API  to not be running"
	}
	pod := pods.Items[0]
	podLogOpts := v1.PodLogOptions{}
	req := cl.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &podLogOpts)
	podLogs, err := req.Stream(context.TODO())
	if err != nil {
		return "error in opening stream"
	}
	defer func() {
		err := podLogs.Close()
		if err != nil {
			log.Print("error in closing log stream")
		}
	}()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "error in copy information from podLogs to buf"
	}
	str := buf.String()

	return str

}

func getTASPolicy(name string, str string, metric string, operator string, target int64) *api.TASPolicy {
	pol := &api.TASPolicy{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: api.TASPolicySpec{
			Strategies: map[string]api.TASPolicyStrategy{
				//Need to have a base deschedule to make the scheduleonmetric policy work correctly.
				//TODO: This should be considered a bug.
				str: {
					PolicyName: name,
					Rules: []api.TASPolicyRule{
						{Metricname: metric, Operator: operator, Target: target},
					},
				},
			},
		},
	}
	if str != dontschedule.StrategyType {
		pol.Spec.Strategies[dontschedule.StrategyType] =
			api.TASPolicyStrategy{
				PolicyName: "filter1",
				Rules: []api.TASPolicyRule{
					{Metricname: "filter1_metric", Operator: "Equals", Target: 2000000},
				},
			}
	}
	return pol
}

func repeatTest(f func(*testing.T), t *testing.T, reps int) {
	for i := 0; i <= reps; i++ {
		f(t)
	}
}
