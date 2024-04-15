# Knavigator

Project Knavigator is a comprehensive framework designed to support developers and operations of cloud systems. It addresses various needs, including testing, troubleshooting, benchmarking, performance analysis, and optimization.

The term "knavigator" is derived from "navigator," with a silent "k" prefix representing "kubernetes." Much like a navigator, this initiative assists in charting a secure route and steering clear of obstacles within the cluster.

## Getting started

If you haven’t built Knavigator, run
```shell
$ make build
```

## Running jobs

### NGC and BCP 

Run a single-node NGC job in BCP simulation framework:
```shell
$ ./bin/knavigator -tasks ./resources/tests/ngc/test-sn-ngcjob.yml
```

Run a multi-node NGC job in BCP simulation framework:
```shell
$ ./bin/knavigator -tasks ./resources/tests/ngc/test-mn-ngcjob.yml
```
### Volcano

1. Install [volcano](https://volcano.sh).

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

2. Run a Volcano batch jobwith [volcano](https://volcano.sh):
```shell
$ ./bin/knavigator -tasks ./resources/tests/volcano/test-job.yml
```
### Native kubernetes

Run a kubernetes job:
```shell
$ ./bin/knavigator -tasks ./resources/tests/k8s/test-job.yml
=======
1. Install [JobSet](https://github.com/kubernetes-sigs/jobset) in your cluster.
```shell
kubectl apply --server-side -f https://github.com/kubernetes-sigs/jobset/releases/download/v0.4.0/manifests.yaml
```

The controller runs in the `jobset-system` namespace. Make sure it is running on a real node, e.g., a control-plane node.

Read the [installation guide](https://jobset.sigs.k8s.io/docs/installation/) to learn more.

2. Run jobset with workers: 
```shell
$ ./bin/knavigator -tasks ./resources/tests/k8s/test-jobset.yml
```

3. Run a test jobset with a driver and workers:
```shell
$ ./bin/knavigator -tasks ./resources/tests/k8s/test-jobset-with-driver.yml
```
