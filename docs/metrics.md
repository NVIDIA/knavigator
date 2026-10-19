# Metrics and Dashboard

Knavigator operates in tandem with the Prometheus node resource exporter, which emits metrics detailing the utilization of resources at the node level. These metrics assist in pinpointing potential bottlenecks or inefficiencies, thereby enhancing the effectiveness of the scheduling framework.
Moreover, Knavigator's functionality relies on a Prometheus server  to execute tasks that evaluate PromQL queries.

## Run Results

The `-results <dir>` flag records every pod that requests `nvidia.com/gpu` while the workflows run. After the last workflow, Knavigator waits for the submitted jobs to finish (up to `-results.timeout`, default 1h) and writes two files:

- `timeline.csv`: one row per pod with its job, node, GPUs, phase, and created, scheduled and finished times.
- `results.json`: failed pods, makespan, GPU-hours, GPU utilization against allocatable GPUs, pods never scheduled, jobs started, and mean, p95 and max wait for pods and jobs.

Knavigator exits with an error after writing the files if the run did not drain, any pod failed, or any job never started, so CI can gate on the exit code.

A job is one object submitted by a `SubmitObj` task whose `RegisterObj` declares `podNameFormat` and `podCount`. Job wait runs from submission until the job's last required pod is scheduled, so it includes time spent in a queue before the pods exist.

```bash
./bin/knavigator -results ./out \
  -workflow 'resources/benchmarks/gang-scheduling/workflows/{config-nodes.yaml,config-volcano.yaml,run-test.yaml}'
```
