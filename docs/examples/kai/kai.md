## Example of running `KAI` with `knavigator`

### Running workflows with `MPI job` and `Job`

Install [KAI scheduler](https://github.com/kai-scheduler/KAI-Scheduler) in your cluster. On a kind cluster with KWOK, `scripts/schedulers/kai.sh` does it:

```shell
./scripts/schedulers/kai.sh     # KAI v0.18.6; set KAI_VERSION to use another release
```

The install adds a stand-in `ClusterPolicy` CRD, which the KAI operator needs and a GPU Operator normally provides, and sets unlimited quota on the default queues.

The Job templates mark each Job as a gang with the `kai.scheduler/batch-min-member` annotation, set to the number of replicas. Without it, KAI schedules a Job's pods one by one and a large Job can hold GPUs while it waits for the rest. Pods join a queue through the `kai.scheduler/queue` label. A template can set a pod priority class with the optional `priorityClassName` parameter, which KAI uses to decide what to preempt.

Run an MPI job:
```shell
./bin/knavigator -workflow resources/workflows/kai/test-mpijob.yaml
```

Run a multi-replica Job:
```shell
./bin/knavigator -workflow resources/workflows/kai/test-job.yaml
```
