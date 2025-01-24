## Example of running `job` and `jobset` with `knavigator`

### Running workflow with a native Kubernetes `job`

```shell
./bin/knavigator -workflow ./resources/workflows/k8s/test-job.yml
```

### Running workflows with `jobset`

Install [JobSet API](https://github.com/kubernetes-sigs/jobset) in your cluster:
```shell
JOBSET_VERSION=v0.8.1
kubectl apply --server-side -f https://github.com/kubernetes-sigs/jobset/releases/download/${JOBSET_VERSION}/manifests.yaml
```

Run a jobset with workers: 
```shell
./bin/knavigator -workflow ./resources/workflows/jobset/test-jobset.yaml
```

Run a jobset with a driver and workers:
```shell
./bin/knavigator -workflow ./resources/workflows/jobset/test-jobset-with-driver.yaml
```
