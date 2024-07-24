## Example of running `kueue` with `knavigator`

Install `kueue` by following these [instructions](https://kueue.sigs.k8s.io/docs/installation/):

```bash
KUEUE_VERSION=v0.8.0
kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/manifests.yaml

kubectl apply -f charts/overrides/kueue/priority.yml
```

Run a kueue job: 
```bash
./bin/knavigator -workflow resources/workflows/kueue/test-job.yml -cleanup
```

Run a preemption workflow with kueue: 
```bash
./bin/knavigator -workflow resources/workflows/kueue/test-preemption.yml -cleanup
```
