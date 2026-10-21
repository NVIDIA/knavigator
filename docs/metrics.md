# Metrics and Dashboard

Knavigator operates in tandem with the Prometheus node resource exporter, which emits metrics detailing the utilization of resources at the node level. These metrics assist in pinpointing potential bottlenecks or inefficiencies, thereby enhancing the effectiveness of the scheduling framework.
Moreover, Knavigator's functionality relies on a Prometheus server  to execute tasks that evaluate PromQL queries.

## Run Results

The `-results <dir>` flag records every pod that requests `nvidia.com/gpu` while the workflows run. After the last workflow, Knavigator waits for the submitted jobs to finish (up to `-results.timeout`, default 1h) and writes two files:

- `timeline.csv`: one row per pod with its job, node, GPUs, phase, and created, scheduled and finished times.
- `results.json`: failed pods, makespan, GPU-hours, GPU utilization against allocatable GPUs, pods never scheduled, jobs started, and mean, p95 and max wait for pods and jobs.

Knavigator exits with an error after writing the files if the run did not drain, any pod failed, or any job never started, so CI can gate on the exit code.

`results.json` also has `jobs_by_gang_size`, one row per pods-per-job count with `submitted`, `waiting` (a pod not yet placed), `running` (all pods placed, some still running), `complete` and `wait_sec` for the jobs that have started. A pod that was preempted and replaced keeps its job running until the replacement ends.

### Prometheus and Grafana

`-results.pushgateway <url>` also sends the run to a [Prometheus Pushgateway](https://github.com/prometheus/pushgateway), so the run shows up next to the node metrics. Knavigator pushes the pod gauges and the summary of the run so far every `-results.push-interval` (default 10s), and the final summary when the run ends. Every series carries a `run` label, set with `-results.run` and defaulting to the start time, so separate runs stay apart. A failed push logs a warning and never fails the run.

`scripts/env.sh` installs a Pushgateway with a ServiceMonitor next to the Prometheus stack. From outside the cluster, reach it with a port-forward:

```bash
kubectl -n monitoring port-forward svc/pushgateway-prometheus-pushgateway 9091 &
./bin/knavigator -results ./out -results.pushgateway http://localhost:9091 -results.run baseline \
  -workflow 'resources/benchmarks/gang-scheduling/workflows/{config-nodes.yaml,config-volcano.yaml,run-test.yaml}'
```

Pod gauges:

- `knavigator_pods_by_state{state}`: GPU pods that are pending, running, succeeded, failed, evicted (deleted after placement) or deleted before placement.
- `knavigator_gpus_in_use` and `knavigator_gpus_pending`: GPUs requested by running pods and by pods not yet placed.
- `knavigator_speedup`: the factor `-speedup` compresses time by (1 without it). The summary gauges already report simulated time, while these pod gauges and the time axis are real time, so multiply any total you compute from them by this factor.

Every push also carries the summary of the run so far, as one gauge per `results.json` field, named `knavigator_<field>`. The final push replaces it with the values for the whole run. Booleans become 0 or 1, and `null` and text fields are skipped. A field with `count`, `mean`, `p95` and `max` gets the label `stat`. A list whose rows have a `bucket` or `gang_size` field gets that field as a label. For example, `job_wait_sec.p95` is `knavigator_job_wait_sec{stat="p95"}`. New `results.json` fields appear with no further change.

During the run these are numbers so far. Makespan is the time since the first submission, utilization covers that span, and job wait counts only jobs that have started, so it reads low while jobs are still queued. With `-speedup`, summary values report simulated time and the pod gauges report real time.

The Pushgateway keeps the last value of a run until you delete it:

```bash
curl -X DELETE http://localhost:9091/metrics/job/knavigator/run/baseline
```

[`resources/dashboards/run-results.json`](../resources/dashboards/run-results.json) charts a run: the numbers so far, a table of jobs by gang size, utilization over the selected time range (from `knavigator_gpus_in_use`), GPUs in use and waiting, pods by state, evictions (pods evicted, jobs that lost a pod, GPU-hours lost, and the scheduler's preemption events), and occupancy with wasted GPUs. The wasted GPU-hours table and pie show whole-run totals that `-results` computes, in simulated hours when the run used `-speedup`, so they do not depend on the dashboard's time range: `wasted_gpu_hours` is `evicted_gpu_hours` (work lost when a scheduler removes pods) plus `binding_gap_gpu_hours` (GPU time gang pods held while the rest of their gang was not yet bound). Idle capacity, GPUs that sit idle while pods wait, is a separate measure at the bottom of the page. The time axis is real time. The eviction tiles read the fields that `-results` reports once pods are preempted, and say "No eviction data" without them. Import it into Grafana next to `GPU-occupancy.json`, and pick the run from the `Run` variable.

`resources/dashboards/GPU-occupancy.json` averages cluster occupancy over the time range as GPUs requested divided by GPUs in the cluster, so nodes that came and went during the range count for the time they existed. It also reports two GPU-hour totals over the time range shown. Wasted GPU-hours counts GPUs that sat idle while pods were waiting for GPUs: at each moment, the smaller of the idle GPUs and the GPUs requested by waiting pods. Idle GPU-hours counts GPUs that no pod requested, whether or not work was waiting. Both come from `node_resource_requests` and kube-state-metrics, so they work without `-results.pushgateway`.

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
