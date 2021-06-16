This folder has a simple example POD which uses kubernetes extended resources

To deploy, you can run in this folder:

```
kubectl apply -f .
```

Then you can check the GPU devices of the first pod in the deployment with:

```
kubectl exec -it deploy/bb-example -- ls /dev/dri
```