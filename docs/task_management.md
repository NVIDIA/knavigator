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

Each job gets these template parameters on top of `params`: `jobId`, `class`, `account`, `gangSize`, `gpus`, `priority`, `runtime` (scaled, as a duration string), and `timeLimitSeconds` and `timeLimitMinutes` (the scaled runtime times `timeLimitFactor`, rounded up).

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

To replay a recorded trace, set `file` to a CSV file instead of `classes`. The header must be `job_id,account,submit_time,duration,gpus,gang_size,priority`. Times are seconds from the start of the trace, `gpus` is per pod and `gang_size` is pods per job. Each job keeps its `job_id` as `jobId` and gets the same template parameters as a generated one, with an empty `class`, so `wait_by_class` is absent. `-speedup` and `timeLimitFactor` apply as above. Every job must request GPUs, since `-results` only records GPU pods, so filter CPU-only rows out first. The file is read when the task is built, so a bad row stops the workflow before any job is submitted. A `nameFormat` such as `trace{{.jobId}}` names each object after its trace row.

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
