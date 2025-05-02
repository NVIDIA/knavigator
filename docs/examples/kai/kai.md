## Example of running `KAI` with `knavigator`

### Running workflows with `MPI job` and `Job`

Install [KAI scheduler](https://github.com/NVIDIA/KAI-Scheduler/blob/main/README.md) in your cluster.

Run an MPI job:
```shell
./bin/knavigator -workflow resources/workflows/kai/test-mpijob.yaml
```

Run a multi-replica Job:
```shell
./bin/knavigator -workflow resources/workflows/kai/test-job.yaml
```
