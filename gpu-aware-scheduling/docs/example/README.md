This folder has simple examples which use kubernetes extended resources and other GAS features.

To deploy, you can run in this folder:

```
kubectl apply -f <test_file_name>
```

Then you can check the GPU devices of the first pod in the deployment with:

```
kubectl exec -it deploy/<deployment name> -- ls /dev/dri
```

Or you can use logs command to check which GPUs the pod is using:

```
kubectl logs --all-containders=true <pod name> 
```

With describe command you can check in which GPUs GAS has scheduled the pods along with the other pod information:

```
kubectl describe pods <deployment name>
```
