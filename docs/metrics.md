# Metrics and Dashboard

Knavigator operates in tandem with the Prometheus node resource exporter, which emits metrics detailing the utilization of resources at the node level. These metrics assist in pinpointing potential bottlenecks or inefficiencies, thereby enhancing the effectiveness of the scheduling framework.
Moreover, Knavigator's functionality relies on a Prometheus server  to execute tasks that evaluate PromQL queries.

## Run Results

The `-results <dir>` flag records every pod that requests `nvidia.com/gpu`, or GPUs through the `gpu.nvidia.com` DRA DeviceClass (`deviceclass.resource.kubernetes.io/gpu.nvidia.com`), while the workflows run. After the last workflow, Knavigator waits for the submitted jobs to finish (up to `-results.timeout`, default 1h) and writes four files:

- `timeline.csv`: one row per pod with its job, node, GPUs, phase, and created, scheduled and finished times.
- `results.json`: failed pods, makespan, GPU-hours, GPU utilization against allocatable GPUs, pods never scheduled, jobs started, and mean, p95 and max wait for pods and jobs.
- `events.csv`: warning events and every eviction or preemption event (`Preempted`, `Preempting`, `Evict`, `Evicted`) since the run started, up to 10000. It shows why a scheduler left work waiting or removed pods.
- `manifest.json`: what ran and where, so that you can keep runs that differ out of one pool. See [Run Manifest](#run-manifest).

`results.json` also reports preemption. `pods_evicted` counts pods deleted after they were placed and before they finished. Schedulers preempt by deleting pods, and job controllers and cleanup delete pods too, so compare it with `preemption_events`, the number of `Preempted` and `Evict` events (one per removed pod). `jobs_evicted` counts jobs that lost a pod this way, and `evicted_gpu_hours` is the GPU time those pods had already used. `evictions_by_priority` groups the evicted pods by the victim's pod priority (`priority`, `pods`, `jobs`, `gpu_hours`), so you can see who a scheduler evicts. `jobs_evicted_again` counts jobs that lost more pods than they have, meaning at least one pod was evicted twice, and `max_evictions_per_job` is the most evictions any one job suffered, with a whole gang evicted at once counting once. `timeline.csv` gains a `priority` column. `binding_gap_gpu_hours` is GPU time that gang pods held while the rest of their gang had not yet been bound, so the gang could not start. `wasted_gpu_hours` is `evicted_gpu_hours` plus `binding_gap_gpu_hours`. `idle_waiting_gpu_hours` is a separate measure of unused capacity: GPU time that sat idle while pods were waiting for GPUs (at each moment, the smaller of the idle GPUs and the GPUs the waiting pods request). All three follow `-speedup`.

A run counts as drained once every job has placed and finished its pods. A pod that a scheduler evicted does not count, so a job finishes only when its replacement does.

`-results.stall <duration>` stops the wait when no pod has been placed or has finished for that long, in real time, so a scheduler that has stopped making progress does not hold the run until `-results.timeout`. The files are written as for a timeout, `stalled` is true in `results.json`, and the exit status is an error. It is off by default. Set it longer than your longest job, because a run of long jobs that are all still running also makes no progress.

Knavigator exits with an error after writing the files if the run did not drain, any pod failed, or any job never started, so CI can gate on the exit code.

A run is only evidence if the cluster under it stayed healthy, so `-results` also checks the cluster and reports it in the `harness` object of `results.json`:

- Before the run, Knavigator refuses to start while pods that request GPUs and are not `Succeeded` or `Failed` already exist, because they hold GPUs the run would count as free. The error names how many and lists them as `namespace/name`.
- On a shared cluster, GPU pods from other users are normal. `-results.allow-leftovers` lets the run start anyway: it logs the same list as a warning, records the GPUs requested by the listed pods that are bound to a node as `leftover_gpus` in the `harness` object, and subtracts them from `gpu_capacity`, which `gpu_utilization` and `idle_waiting_gpu_hours` are computed against, and counts them as in use on the node each pod is bound to for the fragmentation fields (`fragmented_gpu_hours`, `gpu_fragmentation`, `gpu_fragmentation_largest_pod`, `gpu_fragmentation_rack`, `free_nodes_mean`, `nodes_by_free_gpus` and `stranded_gpu_hours`). A pod on a node that is gone at the end of the run is not subtracted, since capacity counts only the nodes there at the end; `leftover_gpus` still includes it. A pod not yet bound to a node holds no GPUs, so it is listed but not subtracted. This is an approximation that can err either way. Bound pods are treated as holding their GPUs for the whole run: if they finish during it, the run had more capacity than counted, so `gpu_utilization` reads high, by at most `leftover_gpus / gpu_capacity` (it can then exceed 1), and `idle_waiting_gpu_hours` can read low. Their nodes also had more free GPUs than counted, so `free_nodes_mean` can read low and the other fragmentation fields can read high or low. An unbound pod that is placed during the run takes GPUs that were counted as free, so `gpu_utilization` reads low and `idle_waiting_gpu_hours` can read high. Its node had fewer free GPUs than counted, so `free_nodes_mean` can read high and the other fragmentation fields can read high or low. If no capacity is left after the subtraction, Knavigator logs a warning and both read 0. It fits long-lived workloads. Without the flag, `leftover_gpus` is left out.
- At the start and end it records the restart count of every container in the control-plane static pods (`kube-system` pods labelled `tier=control-plane`: kube-apiserver, kube-scheduler, kube-controller-manager and etcd on kubeadm and kind), and of every pod in the namespaces given by `-results.restart-namespaces` (comma-separated, for example `kueue-system,kai-scheduler`; default none). Other `kube-system` pods, such as kube-proxy and coredns, are not watched. `control_plane_restarts` lists each container whose count rose as `{container, before, after}`, with `container` written as `namespace/pod/container`, and is empty when none did. Any restart makes Knavigator exit with an error after the files are written. Counts are kept per pod name, so a pod that is deleted and recreated under a new name, such as a Deployment pod evicted during the run, is not flagged. A namespace in `-results.restart-namespaces` that does not exist is watched as empty, with no warning.
- `clock_offset_start_s` and `clock_offset_end_s` are the API server's clock minus the host's, in seconds, measured by creating and deleting a `knavigator-clock-` ConfigMap in `default`. Pod timestamps come from the API server and have one second resolution, so a value between -1 and 1 means the clocks agree. When the start measurement works, the run's start is the API server's own stamp on it, so pods of the run are not taken for pods from before it. A pod stamped before it is left out, so a clock that moves back during the run can leave out pods of the run. A pod still running when results are taken counts until the end of the run, which is no earlier than the end measurement's stamp or any time recorded. When the start measurement puts the API server clock 2s or more off the host's either way, allowing for request latency and one-second stamps, job submissions and the other times Knavigator takes on the host are moved to the API server's clock, and a warning gives the offset. Since the field reads up to 1s low, that takes a `clock_offset_start_s` of about 2s or more ahead, or 3s or more behind, as on a kind cluster whose clock fell behind while the host slept. The offset alone never fails a run, but when the start and end measurements are 2s or more apart the same way, about 3s apart in the two fields, the clock moved during the run, and the run fails after writing its files, since its times are not on one clock. A measurement that fails logs a warning and leaves its field out.
- After the run it checks that no node held more GPUs than it has. It replays each pod's placement and finish on its node against the node's allocatable `nvidia.com/gpu`. GPUs that pods from before the run hold under `-results.allow-leftovers` are left out, since Knavigator does not track when those pods finish, so overbooking that involves them goes unseen. A scheduler that counts a node's GPUs wrongly can bind more GPU pods than the node holds. One way is counting the same GPUs both as device-plugin resources and as DRA devices, which happens when nodes publish both. `overbooked_nodes` is how many nodes went over, `overbooked_peak_gpus` the largest excess on one node at one time, and `overbooked_gpu_hours` the GPU time bound beyond capacity, in simulated time under `-speedup`. Every other metric of such a run is wrong, so any overbooked node makes Knavigator exit with an error after the files are written. Times are cut to the whole second, so a pod that finishes and one that starts on the same node in the same second count as a handover, not an overlap.

`results.json` also has `jobs_by_gang_size`, one row per pods-per-job count with `submitted`, `waiting` (a pod not yet placed), `running` (all pods placed, some still running), `complete` and `wait_sec` for the jobs that have started. A pod that was preempted and replaced keeps its job running until the replacement ends.

`gang_assembly_sec` gives the count, mean, p95 and max time from a gang's first pod placed until all its pods have been placed, over gangs (jobs of more than one pod) that were ever complete. A pod lost before then keeps the gang open until its replacement is placed. A scheduler that places a gang all at once reads 0. One that places pods as room frees up shows how long its gangs sat half placed, and `binding_gap_gpu_hours` gives the GPU time those pods held meanwhile.

### Run Manifest

`manifest.json` follows the manifest format of the Scheduling Capabilities Suite. Knavigator removes an old `manifest.json` first, and writes the new one after the other three files. As a result, a directory that has a `manifest.json` holds a complete set. `run_id` is the name of the directory, so give each run its own directory.

- `schema`: 1.
- `run_id`: the name of the `-results` directory. Pushed metrics carry `-results.run` instead, so to give a pushed run one ID, set `-results.run` to the name of the directory.
- `git_commit` and `git_dirty`: the Git commit that the Knavigator binary was built from, and whether the working tree had changes that were not committed. Untracked files count as changes. Knavigator leaves out both when the binary has no version control stamp, as with `go run`.
- `tools.kubernetes`: the API server's version. If the API server does not give its version, Knavigator logs a warning and leaves out the field.
- `fleet`: the nodes that have more than 0 allocatable `nvidia.com/gpu` at the end of the run. `nodes` is their number, and `gpus_per_node` counts them by size, rounded as in `nodes_by_free_gpus`. For example, `{"8": 16}` is 16 nodes of 8 GPUs. `mode` is `kwok` when all of them are KWOK nodes, `real` when none is, and `mixed` otherwise. Knavigator leaves out `mode` when there is no GPU node. `labels` gives the number of distinct values of each key in `-results.rack-label` and `-results.topology-labels` over these nodes. A key that no GPU node has counts 0. The Suite writes a list of nodes in `fleet` instead, and its readers do not parse `fleet`.
- `profiles`: one entry, named by `-results.profile`, or `unknown` without it.
- `workload`: `workflows`, `vars`, `speedup`, `bsld_floor_sec` and `allow_leftovers`. `workflows` lists the workflows in the order that they ran, and a workflow given twice appears twice. Each entry has `path`, the file that Knavigator read, `digest`, the `sha256:` digest of its text after `-var` substitution, and `templates`. `templates` gives the `sha256:` digest of each template file that the workflow's `RegisterObj` tasks read, by path. Knavigator reads the template files for the digests when the run starts. `vars` has the `-var` values, and `speedup` has `-speedup`. `bsld_floor_sec` is `-results.bsld-floor` in seconds of simulated time, and `allow_leftovers` is `-results.allow-leftovers`. Runs with different values of these settings are not comparable. A trace that `SubmitTrace` replays has its fingerprint in `trace_digest` in `results.json`. The Suite's format has no `workload`, so its readers skip it.

The entry in `profiles` has these fields:

- `start` and `end`: when the recorder started and when Knavigator wrote `manifest.json`, in UTC on the host's clock, as in the Suite. `results.json` measures its times from the first pod or job, on the API server's clock.
- `cluster`: the provider in the nodes' `spec.providerID`, such as `kind`. Knavigator leaves it out unless all nodes that have a `spec.providerID` name the same provider.
- `images`: the image ID of each image reference that the control-plane pods and the pods in `-results.image-namespaces` run at the end of the run. A pod that is replaced during the run, for example by a rollout, appears only with its new image. When pods run one reference under different IDs, the value lists the IDs, sorted and joined with `,`. An image with no known ID, such as an image that is not pulled yet, has the value `""`. `images` leaves out init containers.
- `clock_offset_start_s` and `clock_offset_end_s`: the same values as in `harness`. Knavigator leaves them out when it cannot measure the clock.
- `watch_errors`: the number of times that the recorder may have missed a pod or event change. After the run starts, it counts each failure that stops the pod or event watch, and each relist. A watch that fails because the API server refuses the connection or replies 429 does not count, because client-go retries it from the same point and misses nothing. A relist that the server serves in pages counts once. Some relists count 2, such as a relist whose first attempt fails, so the value can be higher than the number of relists. When `watch_errors` is above 0, Knavigator logs a warning.

`-results.profile` names the scheduler setup under test. It must be a lowercase DNS label, so that the same setup never appears under two names that differ only in case. When you set `-results.profile`, also set `-results.image-namespaces` to the scheduler's namespaces, so that `images` includes the scheduler. Container restarts in these namespaces do not fail the run. To fail the run on them, also add the namespaces to `-results.restart-namespaces`. `-results.topology-labels` takes comma-separated node label keys of topology levels, such as `example.com/block`.

Knavigator writes the `-var` values to `manifest.json` in clear text. Do not pass secrets with `-var` if other people can read the results directory.

### Prometheus and Grafana

`-results.pushgateway <url>` also sends the run to a [Prometheus Pushgateway](https://github.com/prometheus/pushgateway), so the run shows up next to the node metrics. Knavigator pushes the pod gauges and the summary of the run so far every `-results.push-interval` (default 10s), and the final summary when the run ends. If the run stops early, the last push marks it aborted. Every series carries a `run` label, set with `-results.run` and defaulting to the start time, so separate runs stay apart. A failed push logs a warning and never fails the run.

`scripts/env.sh` installs a Pushgateway with a ServiceMonitor next to the Prometheus stack. From outside the cluster, reach it with a port-forward:

```bash
kubectl -n monitoring port-forward svc/pushgateway-prometheus-pushgateway 9091 &
./bin/knavigator -results ./out -results.pushgateway http://localhost:9091 -results.run baseline \
  -workflow 'resources/benchmarks/gang-scheduling/workflows/{config-nodes.yaml,config-volcano.yaml,run-test.yaml}'
```

Pod gauges:

- `knavigator_pods_by_state{state}`: GPU pods that are pending, running, succeeded, failed, evicted (deleted after placement) or deleted before placement.
- `knavigator_gpus_in_use` and `knavigator_gpus_pending`: GPUs requested by running pods and by pods not yet placed.
- `knavigator_run_state{state}`: 1 for the run's current state, `running`, `complete` or `aborted`. The dashboard shows it as the Run state banner. A run that ends on an error or a signal before its results are recorded is `aborted`; a run that recorded its results stays `complete` even if a check such as `-results.stall` then fails.
- `knavigator_speedup`: the factor `-speedup` compresses time by (1 without it). The summary gauges already report simulated time, while these pod gauges and the time axis are real time, so multiply any total you compute from them by this factor.

Every push also carries the summary of the run so far, as one gauge per `results.json` field, named `knavigator_<field>`. The final push replaces it with the values for the whole run. Booleans become 0 or 1, and `null` and text fields are skipped. A field with `count`, `mean`, `p95` and `max` gets the label `stat`. A list whose rows have a `bucket`, `gang_size`, `priority`, `class`, `node_gpus` or `free_gpus` field gets that field as a label, and a list inside such a row adds its own. For example, `job_wait_sec.p95` is `knavigator_job_wait_sec{stat="p95"}`. New `results.json` fields appear with no further change.

The final push carries `knavigator_harness_clock_offset_start_s`, `knavigator_harness_clock_offset_end_s` and, with `-results.allow-leftovers`, `knavigator_harness_leftover_gpus`; the interim pushes already use the reduced capacity. `control_plane_restarts` is not pushed, so a run that failed on a restart looks normal on the dashboard; check the exit code or `results.json`.

During the run these are numbers so far. Makespan is the time since the first submission, utilization covers that span, and job wait counts only jobs that have started, so it reads low while jobs are still queued. With `-speedup`, summary values report simulated time and the pod gauges report real time.

The Pushgateway keeps the last value of a run until you delete it:

```bash
curl -X DELETE http://localhost:9091/metrics/job/knavigator/run/baseline
```

[`resources/dashboards/run-results.json`](../resources/dashboards/run-results.json) charts a run: the numbers so far, a table of jobs by gang size, utilization over the selected time range (from `knavigator_gpus_in_use`), GPUs in use and waiting, pods by state, evictions (pods evicted, jobs that lost a pod, GPU-hours lost, and the scheduler's preemption events), and occupancy with wasted GPUs. The wasted GPU-hours table and pie show whole-run totals that `-results` computes, in simulated hours when the run used `-speedup`, so they do not depend on the dashboard's time range: `wasted_gpu_hours` is `evicted_gpu_hours` (work lost when a scheduler removes pods) plus `binding_gap_gpu_hours` (GPU time gang pods held while the rest of their gang was not yet bound). Idle capacity, GPUs that sit idle while pods wait, is a separate measure at the bottom of the page. The time axis is real time. The eviction tiles read the fields that `-results` reports once pods are preempted, and say "No eviction data" without them. Import it into Grafana next to `GPU-occupancy.json`, and pick the run from the `Run` variable.

[`resources/dashboards/run-comparison.json`](../resources/dashboards/run-comparison.json) compares runs in one table, one row per run: utilization, makespan, jobs started, mean, p95 and max job wait, p95 gang assembly, GPU-hours, idle and wasted GPU-hours, evictions, failures and speedup. Pick the runs from the `Run` variable. A run still in progress shows its numbers so far.

`resources/dashboards/GPU-occupancy.json` averages cluster occupancy over the time range as GPUs requested divided by GPUs in the cluster, so nodes that came and went during the range count for the time they existed. It also reports two GPU-hour totals over the time range shown. Wasted GPU-hours counts GPUs that sat idle while pods were waiting for GPUs: at each moment, the smaller of the idle GPUs and the GPUs requested by waiting pods. Idle GPU-hours counts GPUs that no pod requested, whether or not work was waiting. Both come from `node_resource_requests` and kube-state-metrics, so they work without `-results.pushgateway`.

With `-cleanup`, Knavigator deletes the submitted objects once after the results are written, not at the end of each workflow.

Pod timestamps have one second resolution, so each wait carries about a second of error.

A job is one object submitted by a `SubmitObj` task whose `RegisterObj` declares `podNameFormat` and `podCount`. Job wait runs from submission until the job's last required pod is scheduled, so it includes time spent in a queue before the pods exist.

`results.json` also reports whether a scheduler favors some job sizes. Footprint is a job's total GPUs (GPUs per pod times pods).

- `wait_by_footprint`: job count, jobs started, wait and bounded slowdown per footprint bucket (1, 2, 3-4, 5-8, 9-16, 17+). A job that never created a pod has no footprint, so it counts in `jobs_never_admitted` but not in a bucket.
- `footprint_wait_spearman`: rank correlation of footprint and job wait. Positive means bigger jobs wait longer.
- `large_job_starvation_ratio`: mean wait of jobs of at least one node over mean wait of 1-GPU jobs.
- `bsld_macro`: mean of the per-bucket mean slowdown, equal weight per bucket. Slowdown is max(1, (wait + runtime) / max(runtime, floor)), with the floor set by `-results.bsld-floor` (default 10m, in simulated time under `-speedup`). The value is null unless every job finished.
- `jobs_never_admitted`: jobs with a pod that never got placed. `jobs_unfinished`, `unfinished_gpu_hours`: jobs fully placed but still running when results were taken, and the GPU-hours they used. On a live or stopped run these are the jobs still running, not failures.

Metrics that cannot be computed, such as a correlation over one job, are null.

When jobs are submitted with a `class` parameter, as `SubmitTrace` does for each of its classes, `wait_by_class` has one row per class with `jobs`, `started`, `wait_sec` and `slo_attainment`. `slo_attainment` gives the share of the class's jobs that started within each wait in a fixed ladder: `1m`, `5m`, `15m`, `1h` and `4h`, in simulated time under `-speedup`. A job that never started counts as a miss, so a class that reads 0.98 at `5m` started 98% of its jobs within five minutes. Jobs without a class are left out, and the field is absent when no job has one. The ladder needs no target chosen in advance, so classes with different latency needs, such as inference and training, can be read side by side.

When jobs are submitted with a `traceDigest` parameter, as `SubmitTrace` does for each of its jobs, `trace_digest` is a 12-character fingerprint of the trace's jobs: their IDs, classes, accounts, arrival times, runtimes, GPUs, gang sizes and priorities. Jobs without one are left out, and the field is absent when no job has one. The same seed and classes, or the same file, always give the same digest. In a file, arrival times count from the first job, to the millisecond, so a file shifted in time keeps its digest as long as its times have at most three decimals. A run with several traces lists each once, sorted and comma-separated. Two runs with the same `trace_digest` replayed the same traces, but not necessarily the same workload: the digest leaves out how the workflow replays them, such as `-speedup`, `timeLimitFactor` and how many `SubmitTrace` tasks replay each trace. It also leaves out jobs submitted without one, such as from a plain `SubmitObj`.

`results.json` also shows where the free GPUs were, since free GPUs spread over partly used nodes cannot run a pod that needs a whole node.

- `fragmented_gpu_hours`: free GPU-hours on nodes that were partly in use, in simulated time under `-speedup`.
- `gpu_fragmentation`: `fragmented_gpu_hours` over all free GPU time across the makespan. 0 means every free GPU sat on an empty node, and null means no GPU was free.
- `gpu_fragmentation_largest_pod`: the share of free GPU time on nodes with fewer free GPUs than the run's largest pod requests, so that pod could not have run there. That pod is the largest `gpus` in `timeline.csv`, and one larger than any node makes the field read 1. Unlike `gpu_fragmentation` it counts empty nodes smaller than that pod, so the two differ on a fleet that mixes node sizes. 0 means every free GPU sat on a node with room for that pod, and null means no GPU was free.
- `free_nodes_mean`: time-weighted mean number of nodes with every GPU free.
- `nodes_by_free_gpus`: one row per node size, with `node_gpus`, the number of `nodes` of that size, and `by_free`: for each free GPU count `free_gpus` from all free down to none, `nodes_mean`, the time-weighted mean number of those nodes with that many GPUs free. The counts of a size add up to its `nodes`, and the all-free counts add up to `free_nodes_mean`. [NVIDIA's Volcano bin-packing post](https://developer.nvidia.com/blog/practical-tips-for-preventing-gpu-fragmentation-for-volcano-scheduler/) reports snapshots of nodes with all four GPUs free, 18 before bin-packing and 214 after; this field is the time-weighted mean of that count over the run, for every free count and node size. Pushgateway gets `knavigator_nodes_by_free_gpus_by_free_nodes_mean{node_gpus,free_gpus}`, and `-aggregate` summarizes each pair. The field is absent when the run has no makespan or no node has GPUs.
- `stranded_gpu_hours`: free GPU-hours, while pods waited, on nodes with fewer free GPUs than the smallest waiting pod requests, in simulated time under `-speedup`. `idle_waiting_gpu_hours` caps idle time at what the waiting pods request and this field does not, so it can be larger when many small gaps face few waiting pods.
- `gpu_fragmentation_rack`: with `-results.rack-label <label>`, the share of free GPU time that sat in racks with a GPU in use, where a job that needs a whole rack could not run. Nodes are grouped by that label's value, such as the `example.com/rack` label in the [deployment](deployment.md) example, and nodes without it are left out. Any topology label works, such as a switch or zone. 0 means every free GPU sat in a rack with no GPU in use, and null means the flag was not set or no GPU on a node with the label was free, as when no GPU node carries it. The flag requires `-results` and must be a label key. [resources/workflows/k8s/test-racks.yml](../resources/workflows/k8s/test-racks.yml) labels two racks of two nodes.

Node capacity and racks are read at the end of the run, so a node that joined during it counts as there the whole time, and one that left is not counted at all. With `-results.allow-leftovers`, the GPUs that pods from before the run hold on a node count as in use for the whole run, as described above.

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

### Repeated Runs

One run is one sample. Kueue's mean wait, for example, can move by 10% between identical runs, so a gap of that size between two schedulers says nothing on its own. Repeat each configuration, ideally on a fresh cluster each time, and summarize the runs with `-aggregate`:

```bash
for i in 1 2 3 4 5; do
  ./bin/knavigator -speedup 60 -results ./out/volcano-$i -workflow ...
done
./bin/knavigator -aggregate './out/volcano-*' > volcano.json
```

`-aggregate` reads `results.json` from every directory the glob matches and prints JSON with one entry per metric, named like the Prometheus series without the `knavigator_` prefix, such as `job_wait_sec{stat=p95}`. Each entry has:

- `n`: runs that reported the metric. A null value, such as `bsld_macro` in a run that did not finish, does not count.
- `mean`, `min`, `max`, and `sd`, the sample standard deviation, which is null for a single run.
- `cv`: `sd` over the absolute mean, null when the mean is 0.
- `ci95`: a 95% interval for the mean, from 4000 bootstrap resamples with a fixed seed, so the same runs always give the same interval.
- `stable`: at least two runs, and either no variation or a `cv` of at most 5%. `-aggregate.baseline` below compares two configurations.

Combine or compare only runs made with the same `-results.allow-leftovers` and `-results.rack-label` settings. With `-results.allow-leftovers`, `gpu_capacity` excludes the leftover pods' GPUs, so the utilization of the two kinds of run is not measured against the same capacity. With different `-results.rack-label` values, `gpu_fragmentation_rack` measures different groups, such as racks and zones.

If the runs replayed different traces, `-aggregate` logs a warning, since the spread then includes the difference between the traces as well as the variation between runs. It also warns when only some of the runs report a `trace_digest`: the others, such as runs from before the field existed, may have run any workload.

To compare two configurations, give the runs of one as `-aggregate.baseline`:

```bash
./bin/knavigator -aggregate './out/kueue-*' -aggregate.baseline './out/volcano-*' > kueue-vs-volcano.json
```

Each metric that the baseline also reports gains `vs_baseline`:

- `n`: baseline runs that reported the metric.
- `diff`: the mean minus the baseline mean.
- `ci95`: a 95% interval for `diff`, from 4000 bootstrap resamples of each side with a fixed seed. It is null when only one run on either side reports the metric.
- `clear`: `ci95` excludes 0 and at least 5 runs on each side report the metric. The intervals of two means can overlap while the interval of their difference excludes 0, so read `clear` rather than comparing the two `ci95`.

Take turns between the configurations, one run of each and then the next round, so slow drift on the machine does not read as a difference.

With five runs a side, `clear` is a screen rather than a strict test: between two identical configurations, about one varying metric in eight reads `clear` by chance. Pick the metrics that matter before the runs, and do not read one `clear` among many as a result.

A difference belongs to the configuration only when both sides replayed the same trace, so `-aggregate.baseline` refuses a side whose runs reported different `trace_digest` values and two sides with different ones. It also refuses a run that both globs match, since each side is resampled on its own. When only one side reports a `trace_digest`, or only some runs on a side do, it logs a warning and compares them anyway.
