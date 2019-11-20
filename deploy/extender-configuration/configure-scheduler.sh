## Create config map that we will use as our scheculer extender policy
CONFIG_MAP=scheduler-extender-configmap.yaml
CLUSTER_ROLE=configmap-getter.yaml
kubectl apply -f $CONFIG_MAP
kubectl apply -f $CLUSTER_ROLE
##Add clusterrole binding - default binding edit - to give kube-scheduler access to configmaps in kube-system.
kubectl create clusterrolebinding scheduler-config-map --clusterrole=configmapgetter --user=system:kube-scheduler
## Add arguments to our kube-scheduler manifest

MANIFEST_FILE=/etc/kubernetes/manifests/kube-scheduler.yaml

sed -i '/^    - --policy-configmap/d' $MANIFEST_FILE
sed -e "/    - kube-scheduler/a\\
    - --policy-configmap=scheduler-extender-policy\n    - --policy-configmap-namespace=kube-system" $MANIFEST_FILE -i

## Need to add in dnsPolicy: ClusterFirstWithHostNet here
echo $MANIFEST_FILE

