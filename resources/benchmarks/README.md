# Benchmark Tests

This directory contains benchmark tests for the following workload managers and schedulers:

- Kueue
- Volcano
- Yunikorn
- Run:ai

The benchmark tests involve submitting workloads intended to evaluate the scheduler's performance under specific scenarios.

These workloads are designed to fully utilize the cluster under optimal scheduling conditions.

One approach to benchmarking is to run this workload on clusters with different schedulers and then compare the average GPU occupancy of the nodes.

For all workload managers except Run:ai, the benchmark test involves two sequential workflows. The first workflow registers the CRDs, and the second workflow runs the common part of the test.
Run:ai requires additional customization and thus has a separate workflow

## Gang Scheduling Benchmark Test

The gang-scheduling benchmark workflow operates on 32 virtual GPU nodes, submitting a burst of 53 jobs with replica numbers ranging from 1 to 32 in a [predetermined order](gang-scheduling/workflows/run-test-common.yml).

#### Example

To run the benchmark test for Kueue:

```bash
./bin/knavigator -workflow 'resources/benchmarks/gang-scheduling/workflows/{config-kueue.yml,run-test-common.yml}'
```

#### Run:ai

```bash
./bin/knavigator -workflow resources/benchmarks/gang-scheduling/workflows/run-test-runai.yml
```

## Scaling Benchmark Test

The scaling benchmark workflow operates on 500 virtual GPU nodes, submitting [two workloads](workflows/run-test-common.yml) one after another. The first workload is a job with 500 replicas, the second workload is 500 single node jobs started simultaneously.

### Example

To run the benchmark test for Volcano:

```bash
./bin/knavigator -workflow 'resources/benchmarks/scaling/workflows/{config-volcano.yml,run-test-common.yml}'
```

### Run:ai

```bash
./bin/knavigator -workflow resources/benchmarks/scaling/workflows/run-test-runai.yml
```
