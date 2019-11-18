package cache
//This file gives the autoupdating cache the methods needed to work as a web server and operate remotely.
//This allows the scheduler extender to use the policy controller as a cache.
import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
)

//This adds a 404 return header and correct content type when some unexpected state is hit (incorrect path)
func (n *AutoUpdatingCache) errorHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
}

//This method performs some basic checks on the input and returns if some error is encountered.
func (n *AutoUpdatingCache) prepRequest(w http.ResponseWriter, r *http.Request) (string, error) {
	if r.ContentLength > 10*1000*1000 {
		w.WriteHeader(http.StatusNotFound)
		return "", errors.New("size too large")
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return "", errors.New("method Type not POST")
	}
	urlString := r.URL.Path
	return urlString, nil
}

//This method runs at the metrics endpoint. If the request is formed properly it looks for the metric named in the cache.
func (n *AutoUpdatingCache) getMetric(w http.ResponseWriter, r *http.Request) {
	urlString, err := n.prepRequest(w, r)
	defer func(err error) {
		if err != nil {
			http.Error(w, "Encode error: "+err.Error(), http.StatusBadRequest)
		}
	}(err)
	urlParts := strings.Split(urlString, "/")
	if len(urlParts) != 4 {
		log.Print("Request malformed")
		return
	}
	metricName := urlParts[3]
	metric, err := n.ReadMetric(metricName)
	if err != nil {
		log.Print(err)
		return
	}
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(metric); err != nil {
		http.Error(w, "Encode error", http.StatusBadRequest)
	}
}

//This method runs at the policy endpoint. If the request is formatted properly it looks for the policy namespace/name in the cache.
func (n *AutoUpdatingCache) getPolicy(w http.ResponseWriter, r *http.Request) {
	urlString, err := n.prepRequest(w, r)
	defer func(err error) {
		if err != nil {
			http.Error(w, "Encode error: "+err.Error(), http.StatusBadRequest)
		}
	}(err)
	urlParts := strings.Split(urlString, "/")
	if len(urlParts) != 5 {
		err = errors.New("malformed request")
		log.Print(err)
		return
	}
	namespace, policyName := urlParts[3], urlParts[4]
	policy, err := n.ReadPolicy(namespace, policyName)
	if err != nil {
		log.Print(err)
		return
	}
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(policy); err != nil {
		http.Error(w, "Encode error", http.StatusBadRequest)
	}
}
//Serve sets up the server and routes for the cache and starts to listen on the provided port.
func (n *AutoUpdatingCache) Serve(port string) {
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           nil,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		MaxHeaderBytes:    1000,
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { n.errorHandler(w, r) })
	http.HandleFunc("/cache/metrics/", func(w http.ResponseWriter, r *http.Request) { n.getMetric(w, r) })
	http.HandleFunc("/cache/policies/", func(w http.ResponseWriter, r *http.Request) { n.getPolicy(w, r) })
	err := srv.ListenAndServe()
	if err != nil {
		log.Print("Server connection failed  ")
		panic(err)
	}
}
