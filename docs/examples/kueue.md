# Example of running `kueue` with `knavigator`

## Preparatory step

To ensure proper installation of `kueue`, verify that your cluster does not contain any virtual nodes. If the `kueue` controller is deployed on a virtual node, it will disrupt its functionality.

```bash
kubectl delete node -l type=kwok
```

## Install kueue

Install kueue by following these [instructions](https://kueue.sigs.k8s.io/docs/installation/):

```bash
KUEUE_VERSION=v0.7.0
kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/manifests.yaml

kubectl apply -f charts/overrides/kueue/priority.yml
```

## Run kueue job

```bash
./bin/knavigator -workflow resources/workflows/kueue/test-job.yml -cleanup
```
