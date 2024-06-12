## Example of running `job` and `jobset` with `knavigator`

### Running workflow with a native Kubernetes `job`

```shell
./bin/knavigator -workflow ./resources/workflows/k8s/test-job.yml
```

### Running workflows with `jobset`

Install [JobSet API](https://github.com/kubernetes-sigs/jobset) in your cluster:
```shell
kubectl apply --server-side -f https://github.com/kubernetes-sigs/jobset/releases/download/v0.5.2/manifests.yaml
```

Run a jobset with workers: 
```shell
./bin/knavigator -workflow ./resources/workflows/k8s/test-jobset.yml
```

Run a jobset with a driver and workers:
```shell
./bin/knavigator -workflow ./resources/workflows/k8s/test-jobset-with-driver.yml
```
