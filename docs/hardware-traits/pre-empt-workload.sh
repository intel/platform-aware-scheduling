#!/bin/sh
kubectl delete -f power-hungry-application.yaml

kubectl get pods cloud-network-function -w
