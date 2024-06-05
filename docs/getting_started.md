# Getting started

Build Knavigator, run
```shell
make build
```

## Running jobs

`Knavigator` currently provides templates for different batch jobs, including kubernetes native `job`, `jobset` and Volcano `job`. The templates for [run:ai workloads](https://docs.run.ai/v2.14/admin/workloads/workload-overview-admin/) is under development.

### Volcano

Install [volcano](https://volcano.sh).

Using YAML files:
```shell
kubectl apply -f https://raw.githubusercontent.com/volcano-sh/volcano/master/installer/volcano-development.yaml
```

Using helm:
```shell
helm repo add volcano-sh https://volcano-sh.github.io/helm-charts
helm install volcano volcano-sh/volcano -n volcano-system --create-namespace
```
Please make sure `volcano-admission`, `volcano-controller` and `volcano-scheduler` all are running on real nodes, e.g., control-plane nodes.

Create a priority class if needed:
```shell
kubectl create priorityclass normal-priority --value=100000
```
Run a Volcano batch job with `volcano`:
```shell
./bin/knavigator -workflow ./resources/workflows/volcano/test-job.yml
```
### Native kubernetes

Run a kubernetes job:
```shell
./bin/knavigator -workflow ./resources/workflows/k8s/test-job.yml
```

Install [JobSet](https://github.com/kubernetes-sigs/jobset) in your cluster:
```shell
kubectl apply --server-side -f https://github.com/kubernetes-sigs/jobset/releases/download/v0.5.2/manifests.yaml
```
The controller runs in the `jobset-system` namespace. Make sure it is running on a real node, e.g., a control-plane node.

Run jobset with workers: 
```shell
./bin/knavigator -workflow ./resources/workflows/k8s/test-jobset.yml
```
Run a test jobset with a driver and workers:
```shell
./bin/knavigator -workflow ./resources/workflows/k8s/test-jobset-with-driver.yml
```

### Kueue

Refer to [this document](./examples/kueue/kueue.md) for detailed instructions on how to run `kueue` system with `knavigator`.
