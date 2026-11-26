# Task Management

Tasks are defined in a YAML format or received via HTTP/gRPC protocols. The Task Manager parses these tasks and executes them using the Kubernetes API. Supported tasks include:

- Create/configure/modify virtual nodes
- Create/modify/delete custom resource objects
- Replay a seeded synthetic workload trace, or a recorded one from a CSV file
- Check the spec and status of custom resource objects
- Check the spec and status of nodes
- Check the spec and status of pods
- Run PromQL query
- Sleep for a specified duration

## SubmitTrace

`SubmitTrace` generates a seeded workload from one or more job classes and submits each job from a registered template at its arrival time. Arrivals are Poisson and runtimes are log-normal. The `-speedup` flag divides both, so a trace of several hours can replay in minutes, and the results report the same factor (see [Time Compression](metrics.md#time-compression)). A `speedup` param in the task must match `-speedup`. The same `seed` and classes always give the same trace.

Each job gets these template parameters on top of `params`: `jobId`, `class`, `account`, `gangSize`, `gpus`, `priority`, `runtime` (scaled, as a duration string), `timeLimitSeconds` and `timeLimitMinutes` (the scaled runtime times `timeLimitFactor`, rounded up), and `traceDigest`, which `-results` reports as `trace_digest`.

```yaml
- id: trace
  type: SubmitTrace
  params:
    refTaskId: job
    seed: 0
    timeLimitFactor: 2
    params:
      namespace: default
    classes:
    - name: inference
      jobs: 600
      arrivalInterval: 24      # mean seconds between arrivals
      runtime: {mu: 5.0, sigma: 0.9, max: 1200}  # log-seconds; max in seconds
      gpus: [1]
      priorities: [100, 100, 500]
      accounts:
      - {name: inference, weight: 1}
    - name: training
      jobs: 200
      arrivalInterval: 160
      runtime: {mu: 8.4, sigma: 1.1, max: 14400}
      gpus: [2, 4, 8]
      gangFraction: 0.225
      gangSizes: [2, 2, 4, 8, 16, 48]
      gangGpus: 8              # gang pods claim whole nodes
      priorities: [50]
      accounts:
      - {name: research, weight: 0.6}
      - {name: trading, weight: 0.4}
```

Register the template with `podNameFormat` and `podCount: "{{.gangSize}}"` so `-results` can measure each job's wait.

A class can also arrive in bursts. `start` delays the class's arrivals by that many seconds, and `regular: true` submits its jobs exactly `arrivalInterval` apart, the first at `start`, instead of drawing the gaps at random. With `arrivalInterval: 0`, every job in the class arrives at `start` at once. These two classes add a pile of 40 training jobs at hour 6 and whole-fleet gangs at hours 8 and 16, on top of the classes above:

```yaml
    - name: training-pile
      jobs: 40
      start: 21600
      arrivalInterval: 0
      runtime: {mu: 8.4, sigma: 1.1, max: 14400}
      gpus: [8]
      priorities: [50]
    - name: full-fleet
      jobs: 2
      start: 28800
      arrivalInterval: 28800
      regular: true
      runtime: {mu: 8.4, sigma: 1.1, max: 14400}
      gpus: [8]
      gangFraction: 1
      gangSizes: [64]
      priorities: [50]
```

Each class draws from its own random stream, so adding a class leaves the jobs of the others unchanged.

To replay a recorded trace, set `file` to a CSV file instead of `classes`. The header must be `job_id,account,submit_time,duration,gpus,gang_size,priority`. Times are in seconds, shifted so the first job arrives at 0, which lets an export in epoch seconds replay as is. `gpus` is per pod and `gang_size` is pods per job. Each job keeps its `job_id` as `jobId` and gets the same template parameters as a generated one, with an empty `class`, so `wait_by_class` is absent. `-speedup` and `timeLimitFactor` apply as above. Every job must request GPUs, since `-results` only records GPU pods, so filter CPU-only rows out first. The file is read when the task is built, so a bad row stops the workflow before any job is submitted. A `nameFormat` such as `trace{{.jobId}}` names each object after its trace row.

```yaml
- id: trace
  type: SubmitTrace
  params:
    refTaskId: register
    file: resources/traces/trace.csv
    params:
      namespace: default
```

[resources/workflows/k8s/test-trace.yml](../resources/workflows/k8s/test-trace.yml) replays a six-job trace on two virtual nodes. It has no check task, so run it with `-results`, which waits for the jobs to finish.

## CheckPod

`CheckPod` verifies the pods of a previous `SubmitObj` task, named by `refTaskId`. `status` is the expected pod phase and `nodeLabels` the labels every running pod's node must carry. Without `timeout` the task checks the pods once; with it, the task waits until every pod matches, and fails at the timeout.

`nodeLabelDomains` limits the pods to at most `max` distinct values of the node label `key`, so `max: 1` means all pods run in one domain, such as one rack. It works alone or with `status` and `nodeLabels`; with `nodeLabels` it also needs `status`, such as `Running`, since `nodeLabels` checks only running pods and a pod counts for `nodeLabelDomains` as soon as it is bound. `key` must be set and `max` must be at least 1. When the pods' nodes take more values, the task fails with the count and the values, quoted, in sorted order:

```text
CheckPod/check: pods on 3 distinct values of cloud.provider.com/topology-rack, at most 1 allowed: ["rack1" "rack2" "rack5"]
```

- A pod counts once it has a node, whatever its phase, unless `status` also asks for one. Without `timeout`, a pod with no node fails the task (`pod 'job-1' has no node, so nodeLabelDomains cannot place it`); with `timeout`, the task waits for the pod to get a node, and the spread is judged over all the pods once every one is accounted for. The watch judges each pod as it was when first counted: a pod deleted after it was counted stays counted, and its replacement counts as one more pod, so the watch can finish before every live pod is placed. Check a set of pods that no eviction touches during the watch.
- A node without the label fails the task (`node 'n5' of pod 'job-1' has no label 'cloud.provider.com/topology-rack'`). It is neither counted as one more value nor skipped. A label with an empty value is a value, shown as `""`.

```yaml
- id: check
  type: CheckPod
  params:
    refTaskId: job
    status: Running
    nodeLabelDomains:
      key: cloud.provider.com/topology-rack
      max: 1
    timeout: 2m
```
