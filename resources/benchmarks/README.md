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

## Priority Preemption Matrix Benchmark Test

The preemption-matrix benchmark runs pairs of single-GPU jobs on 256 virtual nodes of 8 GPUs and checks, per pair, whether the second job preempted the first. The pairs are the eight cases of KAI's preempt test (`test/e2e/suites/preempt/preempt_specs.go` in KAI v0.19.0). Each pair has its own queue whose quota admits exactly one 1-GPU job, with no cohort and no borrowing, so a pair's second job runs only if it preempts the first, and no pair can take another pair's GPU. Every pair runs at once:

1. [`config-priorities.yaml`](preemption-matrix/workflows/config-priorities.yaml) creates four PriorityClasses before any job exists. KAI gives a pod whose PriorityClass is missing its default class instead.
2. `run-first-*.yaml` submits every pair's first job and waits until each is Running.
3. `run-second-*.yaml` submits every pair's second job.
4. [`run-hold.yaml`](preemption-matrix/workflows/run-hold.yaml) waits 60 seconds, then the `check-*.yaml` files look at each pair once, in pair order. Knavigator stops at the first check that fails, so a red run names one pair that differs from the table and says nothing about the pairs after it.

A pair is (first job, second job), not (low, high): pairs 4 to 6 start the higher-priority job first, and pairs 7 and 8 use two jobs of the same class. The first job runs for 5 minutes and the second for 3, both counted from when the pod starts. At the check the second job has about 2 minutes left, so a pair that preempted still shows the second job Running and not the first one back. The first job has about 4 minutes left, so a pair that did not preempt still shows the first job Running and not the second one in its place.

| Class | Value | KAI |
|---|---|---|
| `pm-low-p` | 40 | preemptible |
| `pm-high-p` | 41 | preemptible |
| `pm-low-np` | 140 | non-preemptible |
| `pm-high-np` | 141 | non-preemptible |

KAI treats a priority below 100 as preemptible and one of 100 or more as non-preemptible: its PodGroup CRD says "When unspecified, preemptibility is determined by the PriorityClass value - values below 100 are considered preemptible" (`deployments/kai-scheduler/crds/scheduling.run.ai_podgroups.yaml:74`), and its tests use the same `NonPreemptiblePriorityThreshold` (`test/e2e/modules/constant/constant.go:14`). Its test picks random values with the same relations, high = low + 1 (`preempt_specs.go:51-73`). The jobs carry no `kai.scheduler/preemptibility` label, which would override the threshold.

| Pair | First -> second | KAI | KAI source (`preempt_specs.go`) | Kueue | Kueue source |
|---|---|---|---|---|---|
| 1 | `pm-low-p` -> `pm-high-p` | preempts | `It` :88, `ForPodScheduled(highPod)` :107 | preempts | `pkg/scheduler/preemption/policy/preemption_policy.go:31-33`: 41 > 40 |
| 2 | `pm-low-p` -> `pm-high-np` | preempts | :110, `ForPodScheduled(highPod)` :129 | not applicable | |
| 3 | `pm-low-np` -> `pm-high-np` | does not | :132, `ForPodUnschedulable(highPod)` :151 | not applicable | |
| 4 | `pm-high-p` -> `pm-low-p` | does not | :154, `ForPodUnschedulable(lowPod)` :173 | does not | `preemption_policy.go:31-33`: 40 > 41 is false |
| 5 | `pm-high-np` -> `pm-low-np` | does not | :176, `ForPodUnschedulable(lowPod)` :195 | not applicable | |
| 6 | `pm-high-np` -> `pm-low-p` | does not | :198, `ForPodUnschedulable(lowPod)` :217 | not applicable | |
| 7 | `pm-low-p` -> `pm-low-p` | does not | :220, `ForPodUnschedulable(pod2)` :239 | does not | `preemption_policy.go:31`: strictly greater, 40 > 40 is false |
| 8 | `pm-low-np` -> `pm-low-np` | does not | :242, `ForPodUnschedulable(pod2)` :261 | not applicable | |

Kueue line numbers are Kueue v0.20.2. Kueue runs pairs 1, 4 and 7 only, the pairs where both jobs are preemptible. Kueue has no per-job preemptibility: `withinClusterQueue: LowerPriority` lets a job preempt any job of strictly lower priority in its ClusterQueue (`preemption_policy.go:27-33`). The pairs with a non-preemptible job test something Kueue cannot express as it installs. Kueue has an alpha `PreemptionConfig` API (`apis/kueue/v1alpha1/preemptionconfig_types.go`) whose rules can select victims by workload label, but this benchmark does not use it: its feature gate `ConfigurablePreemptions` is off by default (`pkg/features/kube_features.go:1325-1326`), and its CRD is not in the release `manifests.yaml` that `scripts/schedulers/kueue.sh` applies.

KAI's test runs CPU pods in a queue with a CPU quota and limit of 500m (`preempt_specs.go:44-48`). This benchmark asks for GPUs instead: each pair's queue has a GPU quota and limit of 1, and each job asks for 1 GPU. For KAI the pair queues are children of a parent queue with no GPU quota or limit, because KAI checks quota and limit on every ancestor; CPU and memory stay unlimited. For Kueue each pair has a ClusterQueue with a nominal quota of 1 GPU and CPU and memory to spare.

The checks name the object they read, because a scheduler may never create the pod a check would look for:

- Both schedulers ([`check-p.yaml`](preemption-matrix/workflows/check-p.yaml), [`check-np.yaml`](preemption-matrix/workflows/check-np.yaml)): in a pair that preempts, the second job's pod is Running; in a pair that does not, the first job's pod is Running.
- KAI ([`check-kai.yaml`](preemption-matrix/workflows/check-kai.yaml)): in a pair that preempts, the first job's pod is Pending. KAI preempts by deleting the pod (`pkg/scheduler/cache/evictor/default_evictor.go:41`), and the Job controller creates a replacement, which waits in the full queue. In a pair that does not, the second job's pod is Pending.
- Kueue ([`check-kueue.yaml`](preemption-matrix/workflows/check-kueue.yaml)): the first Job of pair 1 and the second Jobs of pairs 4 and 7 have `spec.suspend: true`. Kueue preempts by suspending the Job (`pkg/controller/jobs/job/job_controller.go:170-172`), and creates no pod for a Job it has not admitted.

Every job finishes: a preempted first job runs again once the second is done, and a second job that waited runs once the first is done. With `-results`, a victim's deleted pod counts as evicted (`pods_evicted`), not failed. A KAI run records 18 GPU pods, the 16 jobs plus a replacement pod for each of the two victims; a Kueue run records 7, the 6 jobs plus the pod created when pair 1's first job is admitted again.

#### Example

The run scripts take the case, `matrix`, and a results directory, and pass extra flags through to Knavigator, as the fill-cluster scripts do. Each run needs a fresh cluster with one scheduler: the jobs keep their names after a run, so a second run on the same cluster fails to create them. KAI must be v0.19.0:

```bash
KAI_VERSION=v0.19.0 ./scripts/schedulers/kai.sh
./scripts/benchmarks/preemption-matrix/run-kai.sh matrix ./results-kai-matrix
```

On another fresh cluster, with Kueue:

```bash
./scripts/schedulers/kueue.sh
./scripts/benchmarks/preemption-matrix/run-kueue.sh matrix ./results-kueue-matrix
```

Each check takes its expected value from a `-var`, so a run with a wrong value can show that the check fails: `pair<N>_first_status` and `pair<N>_second_status` for a pod phase, `pair<N>_first_suspend` and `pair<N>_second_suspend` for a Kueue Job, each defaulting to the value in the table. `-var priority_low_p=<n>`, and likewise `priority_high_p`, `priority_low_np` and `priority_high_np`, set a class's value, and `-var within_cluster_queue=Never` turns off Kueue's preemption. A PriorityClass value cannot change once created, so run each on a fresh cluster.

## Preemption Delay Benchmark Test

The preemption-delay benchmark checks KAI's `kai.scheduler/preemption-delay` annotation on 256 virtual nodes of 8 GPUs. It follows KAI v0.19.0's preemption-delay test (`test/e2e/suites/preempt/preempt_delay_specs.go:75-108`): a preemptible job runs, a job one priority above it arrives with a 90-second delay (`:35`, `:92`), the new job stays unschedulable and the running pod is not deleted for the first half of the window (`:97-103`), then the new job is scheduled (`:106`). Two pairs of single-GPU jobs run at once, each in its own queue whose quota and limit admit one 1-GPU job, as in the preemption matrix: the delayed pair, whose second job carries the delay, and a control pair without it, which shows in the same run that the second job preempts at once when there is no delay. KAI's test runs bare pods asking for 500m CPU in a queue whose CPU quota and limit are 500m (`:47-48`, `:76-80`); here each job is a Job asking for 1 GPU in a queue whose GPU quota and limit are 1, so `-results` records its pods and the scenario drains. Both pairs use `pd-low` (40) for the first job and `pd-high` (41) for the second: both below KAI's preemptible threshold of 100, high = low + 1, as in KAI's test (`:51-62`).

1. [`config-priorities.yaml`](preemption-delay/workflows/config-priorities.yaml) creates the two PriorityClasses before any job exists.
2. [`run-first.yaml`](preemption-delay/workflows/run-first.yaml) submits both first jobs and waits until each is Running.
3. [`run-second.yaml`](preemption-delay/workflows/run-second.yaml) submits both second jobs. Only the delayed one has `kai.scheduler/preemption-delay: "90s"`.
4. [`run-window.yaml`](preemption-delay/workflows/run-window.yaml) waits 60 seconds, then [`check-window.yaml`](preemption-delay/workflows/check-window.yaml) looks once: the delayed second job is Pending and the delayed first job Running; the control second job is Running and the control first job Pending.
5. [`run-post-window.yaml`](preemption-delay/workflows/run-post-window.yaml) waits 90 seconds more, 150 seconds after the second jobs, then [`check-post-window.yaml`](preemption-delay/workflows/check-post-window.yaml) looks once: the delayed second job is Running and the delayed first job Pending.

A first job that shows Pending is the replacement for its preempted pod: KAI preempts by deleting the pod (`pkg/scheduler/cache/evictor/default_evictor.go:41`), and the Job controller creates a new one, which waits in the full queue. Knavigator stops at the first check that fails.

Where KAI reads the delay: the podgrouper reads the annotation from the Job and then from the pod, and the first valid value wins (`pkg/podgrouper/podgrouper/plugins/defaultgrouper/default_grouper.go:278-298`). It copies the value into the PodGroup's `spec.preemptionDelay` when it creates the PodGroup (`pkg/podgrouper/podgroup/handler.go:109`), and the scheduler reads only that field (`pkg/scheduler/api/podgroup_info/job_info.go:211-227`). The job template puts the annotation on the delayed second job's pod template, as KAI's test annotates the pod, and the Job controller copies it onto the pod. The value is a Go duration; the PodGroup shows `90s` as `1m30s`.

When the window starts: at the PodGroup's `creationTimestamp`, or at the PodGroup's last eviction when that is later (`job_info.go:216-220`). KAI's doc says the window "is measured from the workload's creation" (`docs/preemption-delay/README.md:35`); in the code the workload is the PodGroup, which the podgrouper creates when it first sees the Job's pod, slightly after the Job. `creationTimestamp` has whole-second precision, so the window can end up to 1 second before creation + delay. The scheduler checks the window once per scheduling cycle, every second by default.

KAI delays preempt, reclaim and consolidation (`FilterWithinPreemptionDelay` at `pkg/scheduler/actions/preempt/preempt.go:61`, `actions/reclaim/reclaim.go:61`, `actions/consolidation/consolidation.go:52`), but not plain allocation (`actions/allocate/allocate.go:50-54`), so a delayed job that fits free capacity starts at once. Here the queue limit keeps the delayed second job off the free GPUs, and both pairs preempt inside their own queue, so this benchmark exercises the preempt path only.

The check times come from the window. The delayed window cannot end before about 89 seconds after submission (90 seconds less the 1 second of truncation), so the window check at 60 seconds comes about 30 seconds before the window ends, two thirds into it; KAI's own test checks only the first half of its window (`:97-103`). Together the two checks pass only for an effective delay between about 61 and about 139 seconds: a shorter one lets the delayed second job preempt before the window check, and a longer one leaves its first job without a replacement pod at the post-window check. Pod creation, PodGroup creation and the first scheduling attempt all fall within about 1 second of submission, so the post-window check at 150 seconds, about 60 seconds after the window ends, holds whichever of them KAI counted from. A first job checked Pending needs its replacement pod to exist, and the Job controller creates it about 10 seconds after KAI deletes the victim. On the test bed, over three runs, both PodGroups were created within a second of submission, KAI preempted for the control pair 0.8 to 1.3 seconds after its PodGroup's creation, and for the delayed pair 90.3 to 90.9 seconds after it. While it held the delayed second job, KAI reported "Workload is within its preemption delay window (1m30s) and may not trigger evictions before" the PodGroup's creation plus 90 seconds. The window check ran 29.9 to 30.9 seconds before the delayed pair's preemption. At the post-window check the delayed second job had been scheduled for 59.0 to 59.4 seconds, with about 2 of its 3 minutes left, and the first job's replacement had existed for 50.0 to 50.4 seconds. A window held about 50 seconds too long goes red there. The second job runs for 3 minutes and the first for 5, both counted from when the pod starts: the delayed second job, which starts when its window ends, still runs at the post-window check, and each first job's replacement still waits.

Every job finishes: each replacement runs once its pair's second job is done. With `-results`, a victim's deleted pod counts as evicted (`pods_evicted`), not failed. A run records 6 GPU pods: the 4 jobs plus a replacement pod for each of the two victims.

KAI only. Kueue v0.20.2 has no preemption delay: `git grep -i -E 'delay|grace|minruntime' v0.20.2 -- apis/kueue/v1beta2 pkg/scheduler/preemption` hits only workload-slice gating, ProvisioningRequest and admission-check retry delays, the topology scheduling gate and `delayedTopologyRequest`, while the same search over KAI v0.19.0's `pkg/scheduler` hits its delay code in `actions/utils/input_jobs.go`. slurm-bridge does not apply either: its Slurm preempts by partition tier, and a pod's PriorityClass never reaches Slurm, so neither pair preempts there.

#### Example

The run script takes the case, `delay`, and a results directory, and passes extra flags through to Knavigator. Each run needs a fresh cluster: the jobs keep their names after a run, so a second run on the same cluster fails to create them. KAI must be v0.19.0:

```bash
KAI_VERSION=v0.19.0 ./scripts/schedulers/kai.sh
./scripts/benchmarks/preemption-delay/run-kai.sh delay ./results-kai-delay
```

Every value comes from a `-var` with the value above as its default: `preemption_delay` (`90s`; `0s` turns the delay off, as KAI ignores a delay of zero, `job_info.go:212-214`), `window_sleep` (`60s`), `post_window_sleep` (`90s`), `first_ttl` (`5m`), `second_ttl` (`3m`), `priority_low` (`40`) and `priority_high` (`41`). The checks' expected phases are `window_delayed_second_status` (`Pending`), `window_delayed_first_status` (`Running`), `window_control_second_status` (`Running`), `window_control_first_status` (`Pending`), `post_delayed_second_status` (`Running`) and `post_delayed_first_status` (`Pending`), so a run with a wrong value can show that a check fails.

## Topology Placement Benchmark Test

The topology-placement benchmark checks that a job asking for one topology domain, preferred or required, lands in one when one has room. It runs on 256 virtual nodes of 8 GPUs in 2 zones of 8 blocks of 8 racks of 2 nodes, labelled with the three levels of KAI v0.19.0's scale test, `cloud.provider.com/topology-zone`, `-block` and `-rack` (`test/e2e/scale/kwok_workload_scenarios.go:37-39`), in its shape of 2 zones, 8 blocks per zone and 2 nodes per rack (`:87-102`). KAI's test needs at least 512 nodes (`:41`), so it has more racks per block. [`config-nodes.yaml`](topology-placement/workflows/config-nodes.yaml) derives the labels from the chart's rack number, so racks 1 to 8 form `block1` and racks 1 to 64 `zone1`. Every value is unique in the cluster (`rack1` to `rack128`, `block1` to `block16`), unlike KAI's test, which repeats `rack1` in every block: a check that counts distinct values of one label then counts domains.

Each scheduler gets its own topology objects ([`config-kai.yaml`](topology-placement/workflows/config-kai.yaml), [`config-kueue.yaml`](topology-placement/workflows/config-kueue.yaml)). KAI has a `Topology` with the three levels, and the jobs carry `kai.scheduler/topology` with `kai.scheduler/topology-preferred-placement` or `-required-placement` on the Job (`pkg/podgrouper/podgrouper/plugins/defaultgrouper/default_grouper.go:86-89`). Kueue has a `Topology` with a fourth level, `kubernetes.io/hostname`, a ResourceFlavor whose `topologyName` names it, and one ClusterQueue with that flavor only; the jobs carry `kueue.x-k8s.io/podset-preferred-topology` or `podset-required-topology` on the pod template (`site/content/en/docs/tasks/run/topology_aware_scheduling.md:70-71`). Each job asks for one level only, because Kueue rejects two (`pkg/controller/jobframework/tas_validation.go:43-58`).

On an empty cluster a scheduler blind to topology can put a small job in one rack by chance, so a check there proves nothing. Here an occupier job runs first ([`run-occupier.yaml`](topology-placement/workflows/run-occupier.yaml)): 32 pods of 6 GPUs, pinned by the label `knavigator.nvidia.com/occupier=true` to both nodes of the first rack of every block. Each of those nodes keeps 2 GPUs free. A blind placement takes those nodes first, one 2-GPU pod each, across racks and blocks: KAI bin-packs, filling the nodes with the fewest free GPUs first (`pkg/scheduler/plugins/nodeplacement/pack.go:47-66`), and Kueue places a job without a topology annotation as unconstrained at its Topology's lowest level, the host, taking the hosts with the least free room first (`pkg/cache/scheduler/tas_flavor_snapshot.go:1749-1756`, `:2071-2079`). The run checks that all 32 occupier pods run on labelled nodes before the first checked job, and again right after the last one is placed.

[`run-checked.yaml`](topology-placement/workflows/run-checked.yaml) then runs four jobs of 2-GPU pods, one at a time: each is submitted once the one before has finished, plus 10 seconds, so every job finds the same free room. An empty rack holds 8 of these pods and every block 58, so each job fits one domain at its level. Nothing preempts, so no checked pod is replaced during its check. Each job is checked with CheckPod `nodeLabelDomains` ([task management](../../docs/task_management.md#checkpod)): every pod Running, and on at most one value of the level's label. Knavigator stops at the first check that fails.

| Job | Asks for | Pods x GPUs | KAI v0.19.0 | KAI source | Kueue v0.20.2 | Kueue source |
|---|---|---|---|---|---|---|
| `c1-preferred-rack` | preferred rack | 8 x 2 | 1 rack | `docs/topology/README.md:29-34`; `pkg/scheduler/plugins/topology/job_filtering.go:427-470`, `:511-538` | 1 rack | tasks doc `:89-95`; `tas_flavor_snapshot.go:1917-1926` |
| `c2-required-rack` | required rack | 8 x 2 | 1 rack | `docs/topology/README.md:22-27`; `job_filtering.go:447-461` | 1 rack | tasks doc `:73-77`; `tas_flavor_snapshot.go:1955-1973` |
| `c3-preferred-block` | preferred block | 20 x 2 | 1 block | as `c1` | 1 block | as `c1` |
| `c4-required-block` | required block | 20 x 2 | 1 block | as `c2` | 1 block | as `c2` |

KAI's line numbers are KAI v0.19.0 and Kueue's are Kueue v0.20.2; "tasks doc" is Kueue's `site/content/en/docs/tasks/run/topology_aware_scheduling.md`. KAI's background-pods design (`docs/developer/designs/background-pods/examples/topology/README.md:19-20`) says what should happen (line 19) and what KAI does (line 20) when the only rack with room needs a reclaim first: KAI places a preferred-rack job across two racks with free nodes and never reclaims. A one-domain result therefore needs a domain with free room, and this benchmark always leaves one, so no checked job needs a reclaim.

KAI's fix for required levels, `f348ba24`, is in no release. It changes where KAI stops looking for a common domain when all candidate nodes share a domain below the required level (`pkg/scheduler/plugins/topology/common.go:55` at `f348ba24`). Here the allocate action offers a job every node in the cluster (`pkg/scheduler/actions/allocate/allocate.go:88`), which spans both zones, so v0.19.0 places `c2` and `c4` as KAI with the fix would: in one domain at the required level. There is no v0.19.0 finding.

On the test bed, in three runs per scheduler, every check passed and every run placed the jobs the same way. Both schedulers put `c1` and `c2` on the two nodes of `rack2`, and `c3` and `c4` in `block1`: KAI over 5 racks, 2 of its pods on the occupier's nodes in `rack1`, and Kueue over 3 empty racks. Each check fails when its job is not asked for topology. With only that job's annotations dropped, one run per job and scheduler:

| Job | KAI, topology dropped | Kueue, topology dropped |
|---|---|---|
| `c1-preferred-rack` | 7 racks, 7 blocks, 2 zones | 4 racks, 4 blocks, 1 zone |
| `c2-required-rack` | 7 racks, 7 blocks, 2 zones | 4 racks, 4 blocks, 1 zone |
| `c3-preferred-block` | 12 blocks, 2 zones | 10 blocks, 2 zones |
| `c4-required-block` | 12 blocks, 2 zones | 10 blocks, 2 zones |

Every one of those pods ran on an occupier node, as the setup intends. KAI's `c1` check, for example, failed with:

```text
CheckPod/c1-one-rack: pods on 7 distinct values of cloud.provider.com/topology-rack, at most 1 allowed: ["rack1" "rack113" "rack17" "rack49" "rack73" "rack89" "rack9"]
```

On Kueue these checks show topology-aware packing, but not that the requested level was the one used. Once Kueue has picked a domain for an explicit request, it walks down the levels below it, putting the pods in as few domains as it can at each one (`pkg/cache/scheduler/tas_flavor_snapshot.go:1532-1543`). Read from the code, an 8-pod job asking for a block or a zone would still land in one empty rack, and the test bed shows it: with `-var c1_preferred=cloud.provider.com/topology-zone`, Kueue still put `c1` in `rack2` and passed, while KAI spread it over 6 racks of `zone1` and failed `c1-one-rack`. Kueue's `c3`, which asked for a block, filled `rack2` and `rack3` and put the rest in `rack4`, the fewest racks it could. KAI does tell the levels apart: inside the domain it picks, it bin-packs, so its `c3` put 2 pods on the occupier's nodes in `rack1`.

[`run-report.yaml`](topology-placement/workflows/run-report.yaml) then waits for the occupier to finish and runs two jobs on the empty cluster. Their placement is recorded and never checked, but each job must still finish, or the run fails:

- `u-unconstrained`, 20 x 2 GPUs, without a topology annotation. On KAI it is placed blind. On Kueue it is still placed by TAS: a job without an annotation in a ClusterQueue whose flavors are all TAS flavors gets an implied, unconstrained topology request (`pkg/scheduler/flavorassigner/tas_flavorassigner.go:256-265`), which Kueue's own test expects to scatter pods over the fullest hosts even when they would fit one rack (`pkg/cache/scheduler/tas_cache_test.go:1520`). On an empty cluster every host has the same room, and the ties fall in topology order.
- `o-oversized`, 512 x 2 GPUs preferring a rack, KAI's scale test job (`test/e2e/scale/kwok_test.go:225-232`, `kwok_workload_scenarios.go:45`). It is 1024 GPUs, exactly one zone: it fits no rack or block, so a scheduler that falls back one level at a time fills one zone.

In each of three runs per scheduler, KAI placed `u-unconstrained` on 5 nodes, 4 pods each, in 5 racks of 5 blocks across both zones, and Kueue on 5 nodes in 3 racks of `block1` (8, 8 and 4 pods). Both placed `o-oversized` on all 128 nodes of `zone1`, 4 pods each.

The 10 seconds after the occupier finishes, like those between the checked jobs, are a margin over a real lag, not a wait for an event. Kueue frees a job's room only once the Job has its `Complete` condition (`pkg/controller/jobframework/reconciler.go:553-560`, `pkg/controller/jobs/job/job_controller.go:418-426`), some time after its last pod has succeeded, which is all the run waits for. No checked result depends on the margin: even while a finished job is still counted, every block keeps room for a 20-pod job and more than 100 racks stay empty. The report-only spreads on Kueue can: without the margin, `u-unconstrained` went onto the occupier's 20 nodes, one pod each. A Kueue `u-unconstrained` spread of one pod on each of the occupier's nodes means the margin was too short, not how Kueue places an unconstrained job.

Their spread is read from the cluster after the run, or from `timeline.csv`'s `node` column with each node's labels. For example, the zone, block and rack of each `u-unconstrained` pod, with a pod count per triple:

```bash
kubectl get pods -l job-name=u-unconstrained -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}' \
  | xargs -r -n1 kubectl get node --no-headers -o \
      'custom-columns=ZONE:.metadata.labels.cloud\.provider\.com/topology-zone,BLOCK:.metadata.labels.cloud\.provider\.com/topology-block,RACK:.metadata.labels.cloud\.provider\.com/topology-rack' \
  | sort | uniq -c
```

Every job finishes. A run records 620 GPU pods: 32 occupier, 8 + 8 + 20 + 20 checked, 20 unconstrained and 512 oversized.

slurm-bridge does not apply. The test bed's Slurm topology puts every virtual node under one leaf switch, so zone, block and rack have no Slurm counterpart, and slurm-bridge submits each pod as its own Slurm job, so no Slurm request covers a Kubernetes Job's pods together.

#### Example

The run scripts take the case, `placement`, and a results directory, and pass extra flags through to Knavigator, as the fill-cluster scripts do. Each run needs a fresh cluster with one scheduler: the jobs keep their names after a run, so a second run on the same cluster fails to create them, and a ResourceFlavor's `topologyName` cannot change. KAI must be v0.19.0:

```bash
KAI_VERSION=v0.19.0 ./scripts/schedulers/kai.sh
./scripts/benchmarks/topology-placement/run-kai.sh placement ./results-kai-topology
```

On another fresh cluster, with Kueue:

```bash
./scripts/schedulers/kueue.sh
./scripts/benchmarks/topology-placement/run-kueue.sh placement ./results-kueue-topology
```

Each check's bound comes from a `-var` defaulting to 1: `c1_max`, `c2_max`, `c3_max` and `c4_max`. Each checked job's level comes from a `-var` too: `c1_preferred` and `c2_required` (default `cloud.provider.com/topology-rack`), `c3_preferred` and `c4_required` (default `cloud.provider.com/topology-block`). An empty value, such as `-var c1_preferred=`, drops that job's topology annotations, all three for KAI, so a run can show that a check fails when the scheduler is not asked for topology.

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
