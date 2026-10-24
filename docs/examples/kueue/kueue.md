## Example of running `kueue` with `knavigator`

Install `kueue` v0.20.2 with `scripts/schedulers/kueue.sh` (or follow these [instructions](https://kueue.sigs.k8s.io/docs/installation/)), then add the workload priority classes the examples use. The templates use the `kueue.x-k8s.io/v1beta2` API.

```bash
./scripts/schedulers/kueue.sh
kubectl apply -f charts/overrides/kueue/priority.yaml
```

Run a Job with kueue:
```bash
./bin/knavigator -workflow resources/workflows/kueue/test-job.yaml -cleanup
```

Run a preemption workflow with kueue:
```bash
./bin/knavigator -workflow resources/workflows/kueue/test-preemption.yaml -cleanup
```

Run a RayJob with kueue:
```bash
./bin/knavigator -workflow resources/workflows/kueue/test-rayjob.yaml -cleanup
```
