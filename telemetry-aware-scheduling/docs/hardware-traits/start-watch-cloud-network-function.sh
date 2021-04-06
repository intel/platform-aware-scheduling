#!/bin/sh
kubectl apply -f cloud-native-network-function.yaml

kubectl get pods -w cloud-network-function
