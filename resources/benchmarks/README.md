# Benchmark Tests

This directory contains benchmark tests for the following workload managers and schedulers:

- Kueue
- Volcano
- Yunikorn
- Kai
- Run:ai

The benchmark tests involve submitting workloads intended to evaluate the scheduler's performance under specific scenarios.

These workloads are designed to fully utilize the cluster under optimal scheduling conditions.

One approach to benchmarking is to run this workload on clusters with different schedulers and then compare the average GPU occupancy of the nodes.

For all workload managers except Run:ai, the benchmark test involves two sequential workflows. The first workflow registers the CRDs, and the second workflow runs the common part of the test.
Run:ai requires additional customization and thus has a separate workflow

## Gang Scheduling Benchmark Test

The gang-scheduling benchmark workflow operates on 32 virtual GPU nodes, submitting a burst of 53 jobs with replica numbers ranging from 1 to 32 in a [predetermined order](gang-scheduling/workflows/run-test.yaml).

#### Example

To run the benchmark test for Kueue:

```bash
./scripts/benchmarks/gang-scheduling/run-kueue.sh
```

To run the benchmark test for Run:ai

```bash
./scripts/benchmarks/gang-scheduling/run-runai.sh
```

## Fill the Cluster Benchmark Test

The fill-cluster benchmark creates one single-GPU job for every GPU on 256 virtual nodes of 8 GPUs, 2048 jobs, and checks that every pod reaches Running. It follows KAI's "Sanity Fill Cluster" scale test (`test/e2e/scale/kwok_test.go:327-352` and `kwok_test_functions.go:89-138` in KAI v0.19.0) and has the same two cases:

- `running`: the scheduler runs while the jobs are created ([`run-submit.yaml`](fill-cluster/workflows/run-submit.yaml), then [`run-check.yaml`](fill-cluster/workflows/run-check.yaml)).
- `paused`: the scheduler is stopped before the jobs are created. Once every pod or job exists, the run holds for 30 seconds, checks once that nothing was placed, starts the scheduler again and checks that every pod reaches Running.

The run checks that every job got placed: every pod reaches Running. It does not check that all the pods ran at the same time. Each pod runs for 6 minutes (`ttl` in `run-submit.yaml`), so only a run whose `placement_span_sec` is under 360 seconds can have had every pod running at once; read it from `results.json`.

The virtual nodes carry the taint `kwok.x-k8s.io/node=fake:NoSchedule`, and the fill jobs tolerate it, so it keeps every pod without the toleration off the virtual nodes. A pod that tolerates every taint can still land there. This matters for KAI: the operator creates the scheduler pod again on resume, and on a virtual node KWOK would fake it as Running with no scheduler process behind it.

| Scheduler | How the run stops it | Expected while stopped | Expected after it resumes | Source |
|---|---|---|---|---|
| KAI | `kai-config` `spec.scheduler.service.enabled: false`, and `true` to resume ([`pause-kai.yaml`](fill-cluster/workflows/pause-kai.yaml), [`resume-kai.yaml`](fill-cluster/workflows/resume-kai.yaml)) | every pod Pending | every pod Running | KAI v0.19.0 `kwok_test_functions.go:89-138`, which toggles the same field (`test/e2e/modules/configurations/scheduler.go`) |
| Kueue | ClusterQueue `fill` `spec.stopPolicy: Hold`, and `None` to resume ([`pause-kueue.yaml`](fill-cluster/workflows/pause-kueue.yaml), [`resume-kueue.yaml`](fill-cluster/workflows/resume-kueue.yaml)) | every Job still `suspend: true` | every pod Running | Kueue v0.20.2 `apis/kueue/v1beta2/clusterqueue_types.go`: a ClusterQueue whose `stopPolicy` is not `None` makes no new reservation |

With `-results`, `placement_span_sec` is how fast the scheduler placed the jobs, and `last_placement_sec` is the time from the start of the run (the first pod created or job submitted) to the last pod placed (see [metrics](../../docs/metrics.md)). The two schedulers resume differently. KAI's operator deletes the scheduler Deployment when it is disabled and creates it again on resume, so a paused KAI run includes a scheduler restart, while Kueue's controller keeps running and only starts admitting. KAI's own test times from the resume patch, restart included; `placement_span_sec` starts at the first placement after the restart, so it leaves the restart out, and `last_placement_sec` keeps it in. Compare a scheduler's paused and running cases with that in mind.

The run files are not specific to a scheduler: a `config-<scheduler>.yaml` that registers a single-GPU job as `register` can reuse them. The job template sets its own queue and tolerates the virtual nodes' taint; `run-submit.yaml` passes only `replicas`, `gpu` and `ttl`. Preemption, including KAI's preemption delay, is not part of this benchmark, and the slurm-bridge templates and configuration live with slurm-bridge, not in this repository.

The paused KAI case was tested with KAI v0.19.0. `scripts/schedulers/kai.sh` installs v0.18.6 unless `KAI_VERSION` says otherwise:

```bash
KAI_VERSION=v0.19.0 ./scripts/schedulers/kai.sh
```

#### Example

The run scripts take the case and a results directory, and pass extra flags through to Knavigator. `NODE_ADDRESS`, when set, becomes the virtual nodes' InternalIP. The scripts run Knavigator from the repository root, because the workflows name their templates relative to it. A relative results directory is resolved from your directory first, but any other relative path, in a pass-through flag such as `-kubeconfig ./kc` or in `KUBECONFIG`, resolves from the repository root; use absolute paths there.

```bash
./scripts/benchmarks/fill-cluster/run-kai.sh paused ./results-kai-paused
./scripts/benchmarks/fill-cluster/run-kueue.sh running ./results-kueue-running
```

`-var jobs=<n>` submits fewer jobs. The paused checks take their expected value from `-var paused_status=<phase>` (KAI, default `Pending`) and `-var paused_suspend=<bool>` (Kueue, default `true`), so a run with a wrong value can show that the check fails.

A paused run that fails before it resumes the scheduler, such as a run with a wrong expected value, leaves the scheduler stopped for every later run on the cluster. Restart it with the command for the scheduler.

KAI:

```bash
kubectl patch configs.kai.scheduler kai-config --type merge -p '{"spec":{"scheduler":{"service":{"enabled":true}}}}'
```

Kueue:

```bash
kubectl patch clusterqueues.kueue.x-k8s.io fill --type merge -p '{"spec":{"stopPolicy":"None"}}'
```

## Mixed Trace Benchmark Test

The mixed trace benchmarks replay a day of inference and training on 64 virtual GPU nodes with a [`SubmitTrace`](../../docs/task_management.md#submittrace) task. The classes compete for the same GPUs, so a run shows how a scheduler trades inference latency against gang progress. With `-results`, `wait_by_class` reports each class's wait separately.

- [`mixed`](mixed/workflows/run-mixed.yaml) is a steady trickle. Inference is 600 short, high-priority, single-GPU jobs. Training is 200 long, low-priority jobs on 2 to 8 GPUs, about a quarter of them gangs of up to 48 nodes, all arriving at random.
- [`burst`](mixed/workflows/run-burst.yaml) adds bursts on top of a steady trickle. Inference and part of the training arrive evenly over the day, 40 training jobs land at once at hours 0 and 12, and gangs of 48 and all 64 nodes arrive at fixed hours. Gang sizes do not divide the fleet evenly, and a 64-node gang can only start once every node is free.

The run scripts replay a trace at 60 times speed and pass extra flags through to Knavigator. `TRACE=burst` selects the burst trace. Volcano and Kueue are supported.

#### Example

```bash
./scripts/benchmarks/mixed/run-volcano.sh -results ./results-volcano
TRACE=burst ./scripts/benchmarks/mixed/run-volcano.sh -results ./results-volcano-burst
```

## Scaling Benchmark Test

The scaling benchmark workflow operates on 700 virtual GPU nodes with tho workflows. The first [workflow](scaling/workflows/run-test-multi.yaml) submits is a job with 700 replicas, the second [workflow](scaling/workflows/run-test-single.yaml) submits a batch of 700 single-node jobs.

### Example

To run the benchmark test for Volcano:

```bash
./bin/knavigator -workflow 'resources/benchmarks/scaling/workflows/{config-nodes.yaml,config-volcano.yaml,run-test-multi.yaml}'
```

To run the benchmark test for Run:ai

```bash
./bin/knavigator -workflow 'resources/benchmarks/scaling/workflows/{config-nodes.yaml,config-runai.yaml,runai-test-single.yaml}'
```

## Network Topology Benchmark Test

The network topology benchmark workflow runs on 12 virtual GPU nodes, arranged to simulate a tree-like network topology.
Out of these, 5 nodes are marked as busy, leaving 7 nodes available. The workflow submits a job with 3 replicas.

From a network connectivity standpoint, the optimal assignment would be nodes n5, n7, and n8, as shown in the following diagram.

![network aware scheduling](../../docs/assets/network-aware-scheduling.png)

### Example

To run the benchmark test for Run:ai

```bash
./bin/knavigator -workflow 'resources/benchmarks/nwtopo/workflows/{config-nodes.yaml,runai-test.yaml}'
```
