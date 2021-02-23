//Package scheduler extender logic contains code to respond call from the http endpoint.
package scheduler

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"

	"github.com/intel/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/telemetry-aware-scheduling/pkg/metrics"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/core"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/dontschedule"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/scheduleonmetric"
	telemetrypolicy "github.com/intel/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	v1 "k8s.io/api/core/v1"

	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

//MetricsExtender holds information on the cache holding scheduling strategies and metrics.
type MetricsExtender struct {
	cache cache.Reader
}

//NewMetricsExtender returns a new metric Extender with the cache passed to it.
func NewMetricsExtender(newCache cache.Reader) MetricsExtender {
	return MetricsExtender{
		cache: newCache,
	}
}

//Does basic validation on the scheduling rule. returns the rule if it seems useful
func (m MetricsExtender) getSchedulingRule(policy telemetrypolicy.TASPolicy) (telemetrypolicy.TASPolicyRule, error) {
	_, ok := policy.Spec.Strategies[scheduleonmetric.StrategyType]
	if ok && len(policy.Spec.Strategies[scheduleonmetric.StrategyType].Rules) > 0 {
		out := policy.Spec.Strategies[scheduleonmetric.StrategyType].Rules[0]
		if len(out.Metricname) > 0 {
			return out, nil
		}
	}
	return telemetrypolicy.TASPolicyRule{}, errors.New("no prioritize rule found for " + policy.Name)
}

//Pulls the dontschedule strategy from a telemetry policy passed to it
func (m MetricsExtender) getDontScheduleStrategy(policy telemetrypolicy.TASPolicy) (dontschedule.Strategy, error) {
	rawStrategy := policy.Spec.Strategies[dontschedule.StrategyType]
	if len(rawStrategy.Rules) == 0 {
		return dontschedule.Strategy{}, errors.New("no dontschedule strategy found")
	}
	strat := (dontschedule.Strategy)(rawStrategy)
	return strat, nil
}

//prioritize nodes implements the logic for the prioritize scheduler call.
func (m MetricsExtender) prioritizeNodes(args ExtenderArgs) *HostPriorityList {
	policy, err := m.getPolicyFromPod(&args.Pod)
	if err != nil {
		log.Print(err)
		return &HostPriorityList{}
	}
	scheduleRule, err := m.getSchedulingRule(policy)
	if err != nil {
		log.Print(err)
		return &HostPriorityList{}
	}
	chosenNodes, err := m.prioritizeNodesForRule(scheduleRule, args.Nodes)
	if err != nil {
		log.Print(err)
		return &HostPriorityList{}
	}
	log.Printf("node priorities returned: %v", chosenNodes)
	return &chosenNodes
}

//prioritizeNodesForRule returns the nodes listed in order of priority after applying the appropriate telemetry rule rule.
//Priorities are ordinal - there is no relationship between the outputted priorities and the metrics - simply an order of preference.
func (m MetricsExtender) prioritizeNodesForRule(rule telemetrypolicy.TASPolicyRule, nodes *v1.NodeList) (HostPriorityList, error) {
	filteredNodeData := metrics.NodeMetricsInfo{}
	nodeData, err := m.cache.ReadMetric(rule.Metricname)
	if err != nil {
		return nil, fmt.Errorf("failed to prioritize: %v, %v ", err, rule.Metricname)
	}
	// Here we pull out nodes that have metrics but aren't in the filtered list
	for _, node := range nodes.Items {
		if v, ok := nodeData[node.Name]; ok {
			filteredNodeData[node.Name] = v
		}
	}
	outputNodes := HostPriorityList{}
	metricsOutput := fmt.Sprintf("%v for nodes: ", rule.Metricname)
	orderedNodes := core.OrderedList(filteredNodeData, rule.Operator)
	for i, node := range orderedNodes {
		metricsOutput = fmt.Sprint(metricsOutput, " [ ", node.NodeName, " :", node.MetricValue.AsDec(), "]")
		outputNodes = append(outputNodes, HostPriority{node.NodeName, 10 - i})
	}
	log.Print(metricsOutput)
	return outputNodes, nil
}

//filterNodes takes in the arguments for the scheduler and filters nodes based on the pod's dontschedule strategy - if it has one in an attached policy.
func (m MetricsExtender) filterNodes(args ExtenderArgs) *ExtenderFilterResult {
	availableNodeNames := ""
	filteredNodes := []v1.Node{}
	failedNodes := FailedNodesMap{}
	result := ExtenderFilterResult{}
	policy, err := m.getPolicyFromPod(&args.Pod)
	if err != nil {
		log.Print(err)
		return nil
	}
	strat, err := m.getDontScheduleStrategy(policy)
	if err != nil {
		log.Print(err)
		return nil
	}
	violatingNodes := strat.Violated(m.cache)
	if len(args.Nodes.Items) == 0 {
		log.Print("No nodes to compare ")
		return nil
	}
	for _, node := range args.Nodes.Items {
		if _, ok := violatingNodes[node.Name]; ok {
			failedNodes[node.Name] = strings.Join([]string{"Node violates"}, policy.Name)
		} else {
			filteredNodes = append(filteredNodes, node)
			availableNodeNames = availableNodeNames + node.Name + " "
		}
	}
	nodeNames := strings.Split(availableNodeNames, " ")
	result = ExtenderFilterResult{
		Nodes: &v1.NodeList{
			Items: filteredNodes,
		},
		NodeNames:   &nodeNames,
		FailedNodes: failedNodes,
		Error:       "",
	}
	if len(availableNodeNames) > 0 {
		log.Printf("Filtered nodes for %v : %v", policy.Name, availableNodeNames)
	}
	return &result
}

//getPolicyFromPod returns the policy associated with a pod, if declared, from the api.
func (m MetricsExtender) getPolicyFromPod(pod *v1.Pod) (telemetrypolicy.TASPolicy, error) {
	if policyName, ok := pod.Labels["telemetry-policy"]; ok {
		policy, err := m.cache.ReadPolicy(pod.Namespace, policyName)
		if err != nil {
			return telemetrypolicy.TASPolicy{}, err
		}
		return policy, nil
	}
	return telemetrypolicy.TASPolicy{}, fmt.Errorf("no policy found in pod spec for pod %v", pod.Name)
}

//prescheduleChecks performs checks to ensure a pod is suitable for the extender.
//this method will return pods as supplied if they have no declared policy
func (m MetricsExtender) prescheduleChecks(w http.ResponseWriter, r *http.Request) (ExtenderArgs, http.ResponseWriter, error) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return ExtenderArgs{}, w, errors.New("method Type not POST")
	}
	if r.ContentLength > 1*1000*1000*1000 {
		w.WriteHeader(http.StatusInternalServerError)
		return ExtenderArgs{}, w, errors.New("request size too large")
	}
	requestContentType := r.Header.Get("Content-Type")
	if requestContentType != "application/json" {
		w.WriteHeader(http.StatusNotFound)
		return ExtenderArgs{}, w, errors.New("request content type not application/json")
	}
	extenderArgs, err := m.decodeExtenderRequest(r)
	if err != nil {
		log.Printf("cannot decode request %v", err)
		return ExtenderArgs{}, w, err
	}
	if _, ok := extenderArgs.Pod.Labels["telemetry-policy"]; !ok {
		err = fmt.Errorf("no policy associated with pod")
		w.WriteHeader(http.StatusBadRequest)
		return ExtenderArgs{}, w, err
	}
	return extenderArgs, w, err
}

//decodeExtenderRequest reads the json request into the expected struct.
//It returns an error of the request is not in the required format.
func (m MetricsExtender) decodeExtenderRequest(r *http.Request) (ExtenderArgs, error) {
	var args ExtenderArgs
	if r.Body == nil {
		return args, fmt.Errorf("request body empty")
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&args); err != nil {
		return args, fmt.Errorf("error decoding request: %v", err)
	}
	err := r.Body.Close()
	if err != nil {
		return args, err
	}
	return args, nil
}

//writeFilterResponse takes the ExtenderFilterResults struct and writes it as a http response if valid.
func (m MetricsExtender) writeFilterResponse(w http.ResponseWriter, result *ExtenderFilterResult) {
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(result); err != nil {
		http.Error(w, "Encode error", http.StatusBadRequest)
	}
}

//Write out the results of prioritize in the response to the scheduler.
func (m MetricsExtender) writePrioritizeResponse(w http.ResponseWriter, result *HostPriorityList) {
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(result); err != nil {
		http.Error(w, "Encode error ", http.StatusBadRequest)
	}
}

//Prioritize manages all prioritize requests from the scheduler extender.
//It decodes the package, checks its policy, and performs error checking.
//It then calls the prioritize logic and writes a response to the scheduler.
func (m MetricsExtender) Prioritize(w http.ResponseWriter, r *http.Request) {
	log.Print("Received prioritize request")
	extenderArgs, w, err := m.prescheduleChecks(w, r)
	if err != nil {
		log.Printf("failed to prioritze %v", err)
		return
	}
	prioritizedNodes := m.prioritizeNodes(extenderArgs)
	if prioritizedNodes == nil {
		w.WriteHeader(http.StatusNotFound)
	}
	m.writePrioritizeResponse(w, prioritizedNodes)
}

//Filter manages all filter requests from the scheduler. It decodes the request, checks its policy and registers it.
//It then calls the filter logic and writes a response to the scheduler.
func (m MetricsExtender) Filter(w http.ResponseWriter, r *http.Request) {
	log.Print("filter request recieved")
	extenderArgs, w, err := m.prescheduleChecks(w, r)
	if err != nil {
		log.Printf("cannot filter %v", err)
		return
	}
	filteredNodes := m.filterNodes(extenderArgs)
	if filteredNodes == nil {
		log.Print("No filtered nodes returned")
		w.WriteHeader(http.StatusNotFound)
	}
	m.writeFilterResponse(w, filteredNodes)
}

//error handler deals with requests sent to an invalid endpoint and returns a 404.
func (m MetricsExtender) errorHandler(w http.ResponseWriter, r *http.Request) {
	log.Print("unknown path")
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
}

//Check symlinks checks if a file is a simlink and returns an error if it is.
func checkSymLinks(filename string) error {
	info, err := os.Lstat(filename)
	if err != nil {
		return err
	}
	if info.Mode() == os.ModeSymlink {
		return err
	}
	return nil
}

// StartServer starts the HTTP server needed for scheduler.
// It registers the handlers and checks for existing telemetry policies.
func (m MetricsExtender) StartServer(port string, certFile string, keyFile string, caFile string, unsafe bool) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { m.errorHandler(w, r) })
	http.HandleFunc("/scheduler/prioritize", func(w http.ResponseWriter, r *http.Request) { m.Prioritize(w, r) })
	http.HandleFunc("/scheduler/filter", func(w http.ResponseWriter, r *http.Request) { m.Filter(w, r) })
	var err error
	if unsafe {
		log.Printf("Extender Listening on HTTP  %v", port)
		err = http.ListenAndServe(":"+port, nil)
	} else {
		err := checkSymLinks(certFile)
		if err != nil {
			panic(err)
		}
		err = checkSymLinks(keyFile)
		if err != nil {
			panic(err)
		}
		err = checkSymLinks(caFile)
		if err != nil {
			panic(err)
		}
		log.Printf("Extender Now Listening on HTTPS  %v", port)
		srv := configureSecureServer(port, caFile)
		log.Fatal(srv.ListenAndServeTLS(certFile, keyFile))
	}
	log.Printf("Scheduler extender failed %v ", err)
}

//Configuration values including algorithms etc for the TAS scheduling endpoint.
func configureSecureServer(port string, caFile string) *http.Server {
	caCert, err := ioutil.ReadFile(caFile)
	if err != nil {
		log.Fatal(err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	cfg := &tls.Config{
		MinVersion:			tls.VersionTLS12,
		CurvePreferences:		[]tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		ClientCAs:			caCertPool,
		ClientAuth:			tls.RequireAndVerifyClientCert,
		PreferServerCipherSuites: 	true,
		InsecureSkipVerify:       	false,
		CipherSuites:			[]uint16{
			// tls 1.2
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			// tls 1.3 configuration not supported
		},
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           nil,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		MaxHeaderBytes:    1000,
		TLSConfig:         cfg,
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}
	return srv
}
