#!/bin/sh
## Set the location of the kube-scheduler manifest file. This is the location of the file under an ordinary Kubeadm set up.
## Note this step will only work if the scheduler is running in the cluster. If it's running as a service/binary/ex-K8s container the flags will need to be applied separately.

MANIFEST_FILE=/etc/kubernetes/manifests/kube-scheduler.yaml

## Create the config map and the cluster role for the scheduler configuration. The cluster role is needed in Kubeadm to ensure the scheduler can access configmaps.
CONFIG_MAP=scheduler-extender-configmap.yaml
CLUSTER_ROLE=configmap-getter.yaml

kubectl apply -f $CONFIG_MAP
kubectl apply -f $CLUSTER_ROLE
##Add clusterrole binding - default binding edit - to give kube-scheduler access to configmaps in kube-system.
kubectl create clusterrolebinding scheduler-config-map --clusterrole=configmapgetter --user=system:kube-scheduler

## Remove arguments from Kubernetes Scheduler file if they exist
sed -i '/^    - --policy-configmap/d' $MANIFEST_FILE
sed -i '/^  dnsPolicy: ClusterFirstWithHostNet/d' $MANIFEST_FILE
sed -i '/certs/d' $MANIFEST_FILE
sed -i '/name: certdir/d' $MANIFEST_FILE
sed -i '/hostPath/d'  $MANIFEST_FILE

## Copy client cert/key pair into kube-scheduler
mkdir /etc/certs/
cp /etc/kubernetes/pki/ca.key /etc/certs/client.key
cp /etc/kubernetes/pki/ca.crt /etc/certs/client.crt

## Add arguments to our kube-scheduler manifest. The arguments are:
## 1) Policy configmap extender as arg to binary.
## 2) Policy configmap namespace as arg to binary.
## 3) dnsPolicy as part of Pod spec allowing access to kubernetes services.
## 4) Set autorization certs

sed -e "/    - kube-scheduler/a\\
    - --policy-configmap=scheduler-extender-policy\n    - --policy-configmap-namespace=kube-system" $MANIFEST_FILE -i
sed -e "/spec/a\\
  dnsPolicy: ClusterFirstWithHostNet" $MANIFEST_FILE -i
sed -e "/      readOnly: true/a\\
    - mountPath: /host/certs\n      name: certdir" $MANIFEST_FILE -i
sed -e "/  volumes:/a\\
  - hostPath:\n      path: /etc/certs\n    name: certdir\n  - hostPath:" $MANIFEST_FILE -i
