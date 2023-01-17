// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package extender contains types and logic to respond to requests from a Kubernetes http scheduler extender.
package extender

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"time"

	"k8s.io/klog/v2"
)

const (
	l2           = 2
	readTimeout  = 5
	writeTimeout = 10
	maxHeader    = 1000
)

// postOnly check if the method type is POST.
func postOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			klog.V(l2).InfoS("method Type not POST", "component", "extender")

			return
		}

		next.ServeHTTP(w, r)
	}
}

// contentLength check the if the request size is adequate.
func contentLength(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > 1*1000*1000*1000 {
			w.WriteHeader(http.StatusInternalServerError)
			klog.V(l2).InfoS("request size too large", "component", "extender")

			return
		}

		next.ServeHTTP(w, r)
	}
}

// requestContentType verify the content type of the request.
func requestContentType(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestContentType := r.Header.Get("Content-Type")
		if requestContentType != "application/json" {
			w.WriteHeader(http.StatusNotFound)
			klog.V(l2).InfoS("request content type not application/json", "component", "extender")

			return
		}

		next.ServeHTTP(w, r)
	}
}

/*
handlerWithMiddleware runs each function in sequence starting from the outermost function. These middleware functions
are pass/fail checks on the scheduling request. If a check fails the response with an appropriate error is immediately
written and returned.
i.e. with this version of the code:
	return requestContentType(
			contentLength(
				postOnly(handle),
				),
			)
if the content type is not correct - i.e. NOT application/json - the response will be written. contentLength or postOnly
will not run.
*/

// handlerWithMiddleware is handler wrapped with middleware to serve the prechecks at endpoint.
func handlerWithMiddleware(handle http.HandlerFunc) http.HandlerFunc {
	return requestContentType(
		contentLength(
			postOnly(handle),
		),
	)
}

// error handler deals with requests sent to an invalid endpoint and returns a 404.
func errorHandler(w http.ResponseWriter, r *http.Request) {
	klog.V(l2).InfoS("Requested resource: '"+r.URL.Path+"' not found", "component", "extender")
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
}

// StartServer starts the HTTP server needed for the scheduler extender.
// It registers the handlers and checks for existing telemetry policies.
func (m Server) StartServer(port string, certFile string, keyFile string, caFile string, unsafe bool) {
	mx := http.NewServeMux()
	mx.HandleFunc("/", handlerWithMiddleware(errorHandler))
	mx.HandleFunc("/scheduler/prioritize", handlerWithMiddleware(m.Prioritize))
	mx.HandleFunc("/scheduler/filter", handlerWithMiddleware(m.Filter))
	mx.HandleFunc("/scheduler/bind", handlerWithMiddleware(m.Bind))

	var err error

	if unsafe {
		klog.V(l2).InfoS("Extender Listening on HTTP "+port, "component", "extender")

		srv := &http.Server{
			Addr:              ":" + port,
			Handler:           mx,
			ReadHeaderTimeout: readTimeout * time.Second,
			WriteTimeout:      writeTimeout * time.Second,
			MaxHeaderBytes:    maxHeader,
		}

		err = srv.ListenAndServe()
		if err != nil {
			klog.V(l2).InfoS("Listening on HTTP failed: "+err.Error(), "component", "extender")
		}
	} else {
		srv := configureSecureServer(port, caFile)
		srv.Handler = mx
		klog.V(l2).InfoS("Extender Listening on HTTPS "+port, "component", "extender")

		klog.Fatal(srv.ListenAndServeTLS(certFile, keyFile))
	}

	klog.V(l2).InfoS("Scheduler extender server failed to start "+err.Error(), "component", "extender")
}

// Configuration values including algorithms etc for the TAS scheduling endpoint.
func configureSecureServer(port string, caFile string) *http.Server {
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		klog.V(l2).InfoS("caCert read failed: "+err.Error(), "component", "extender")
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	cfg := &tls.Config{
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		ClientCAs:                caCertPool,
		ClientAuth:               tls.RequireAndVerifyClientCert,
		PreferServerCipherSuites: true,
		InsecureSkipVerify:       false,
		CipherSuites: []uint16{
			// tls 1.2
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			// tls 1.3 configuration not supported
		},
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           nil,
		ReadHeaderTimeout: readTimeout * time.Second,
		WriteTimeout:      writeTimeout * time.Second,
		MaxHeaderBytes:    maxHeader,
		TLSConfig:         cfg,
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	return srv
}
