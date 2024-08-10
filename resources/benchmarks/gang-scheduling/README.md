# Gang Scheduling Benchmark Test

This directory contains gang scheduling benchmark tests for the following workload managers and schedulers:

- Jobset
- Kueue
- Volcano
- Yunikorn
- Run:ai

The gang-scheduling benchmark workflow operates on 32 virtual GPU nodes, submitting a burst of 53 jobs with replica numbers ranging from 1 to 32 in a [predetermined order](workflows/run-test-common.yml).

The workload is designed to fully utilize the cluster under optimal scheduling conditions.

One method to perform benchmarking is to input this workload into clusters that use different schedulers and then compare the average GPU occupancy of the nodes.

## Usage

For all workload managers except Run:ai, the benchmark test involves two sequential workflows. The first workflow registers the CRDs, and the second workflow runs the common part of the test.

### Example

To run the benchmark test for Kueue:

```bash
./bin/knavigator -workflow resources/benchmarks/gang-scheduling/workflows/config-kueue.yml,resources/benchmarks/gang-scheduling/workflows/run-test-common.yml
```

### Run:ai

Run:ai requires additional customization and thus has a separate workflow:

```bash
./bin/knavigator -workflow resources/benchmarks/gang-scheduling/workflows/run-test-runai.yml
```
