# Metrics and Dashboard

Knavigator operates in tandem with the Prometheus node resource exporter, which emits metrics detailing the utilization of resources at the node level. These metrics assist in pinpointing potential bottlenecks or inefficiencies, thereby enhancing the effectiveness of the scheduling framework.
Moreover, Knavigator's functionality relies on a Prometheus server  to execute tasks that evaluate PromQL queries.

## Run Results

The `-results <dir>` flag records every pod that requests `nvidia.com/gpu` while the workflows run. After the last workflow, Knavigator waits for the submitted jobs to finish (up to `-results.timeout`, default 1h) and writes two files:

- `timeline.csv`: one row per pod with its job, node, GPUs, phase, and created, scheduled and finished times.
- `results.json`: failed pods, makespan, GPU-hours, GPU utilization against allocatable GPUs, pods never scheduled, jobs started, and mean, p95 and max wait for pods and jobs.

Knavigator exits with an error after writing the files if the run did not drain, any pod failed, or any job never started, so CI can gate on the exit code.

With `-cleanup`, Knavigator deletes the submitted objects once after the results are written, not at the end of each workflow.

A job is one object submitted by a `SubmitObj` task whose `RegisterObj` declares `podNameFormat` and `podCount`. Job wait runs from submission until the job's last required pod is scheduled, so it includes time spent in a queue before the pods exist.

```bash
./bin/knavigator -results ./out \
  -workflow 'resources/benchmarks/gang-scheduling/workflows/{config-nodes.yaml,config-volcano.yaml,run-test.yaml}'
```

### Time Compression

`-speedup N` (requires `-workflow`) shrinks simulated time so long traces finish sooner. Knavigator divides every `Sleep` timeout and the `ttl` param of every `SubmitObj` task by N, and replays `SubmitTrace` arrivals and runtimes N times faster, so jobs run and arrive N times faster. `results.json` reports makespan, GPU-hours and waits in simulated seconds and records `speedup`. GPU utilization is a ratio and does not change. `timeline.csv` keeps real timestamps.

```bash
./bin/knavigator -speedup 60 -results ./out -workflow ...
```

A compressed run shows how jobs stack up and how a scheduler places them. It does not measure scheduler latency:

- The scheduler and its controllers still run in real time. Leases, backoff, resync periods and rate limits do not shrink, so their effect on wait time is under-represented.
- The scheduler sees N times more arrivals and completions per real second. A scheduler that cannot keep up queues work a real-time run would not, so a high speedup can make it look worse than it is. Check that the results stay stable as N grows.
- Only `Sleep` timeouts, the `ttl` param and `SubmitTrace` scale. Timeouts on `CheckPod`, `CheckObj` and `Configure` tasks, `-results.timeout` and `-cleanup.timeout` stay in real time. A runtime parameter with another name, such as `duration`, does not scale.
- Wait times measured in simulated seconds resolve to N times the real-time resolution. A difference between two configurations of a few times N seconds is not a measurement.

Use a speedup to compare configurations quickly. Run at `-speedup 1` before drawing conclusions about wait or latency.
