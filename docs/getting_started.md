# Getting started

## Build Knavigator

```shell
make build
```

## Running jobs

### Preparatory step

To properly install a scheduling framework or workload manager, ensure your cluster has no virtual nodes. Deploying the workload manager on a virtual node will cause it to malfunction.

If you have already created virtual nodes or run some workloads, consider deleting these nodes.
```bash
kubectl delete node -l type=kwok
```

## Tested workflows

In general, `Knavigator` is compatible with any Kubernetes scheduling framework.

We have tested several of these and offer templates and workflows to support them.
* [Job and JobSet](./examples/jobset/jobset.md)
* [Volcano](./examples/volcano/volcano.md)
* [Kueue](./examples/kueue/kueue.md)
* [YuniKorn](./examples/yunikorn/yunikorn.md)
* [Run:ai](./examples/runai/runai.md)
