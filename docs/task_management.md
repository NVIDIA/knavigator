# Task Management

Tasks are defined in a YAML format or received via HTTP/gRPC protocols. The Task Manager parses these tasks and executes them using the Kubernetes API. Supported tasks include:

- Create/configure/modify virtual nodes
- Create/modify/delete custom resource objects
- Replay a seeded synthetic workload trace
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
