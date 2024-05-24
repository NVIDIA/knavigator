# Example of running `kueue` with `knavigator`

## Preparatory step

To ensure proper installation of `kueue`, verify that your cluster does not contain any virtual nodes. If the `kueue` controller is deployed on a virtual node, it will disrupt its functionality.

```bash
helm delete virtual-nodes
```

## Install kueue

Install kueue by following these [instructions](https://kueue.sigs.k8s.io/docs/installation/):

```bash
KUEUE_VERSION=v0.6.2
kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/manifests.yaml
kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/prometheus.yaml

kubectl apply -f charts/overrides/kueue/priority.yml
```

## Deploy cluster and local queues

```bash
kubectl apply -f docs/examples/kueue/queues.yml
```

## Deploy virtual nodes

In this example we deploy 4 GPU nodes. Refer to [values.yaml](values.yaml) for more details.

```bash
helm install virtual-nodes charts/virtual-nodes -f docs/examples/kueue/values.yaml
```

## Run kueue job

```bash
./bin/knavigator -tasks resources/tests/kueue/test-job.yml
```
