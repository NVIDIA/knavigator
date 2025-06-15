Cluster setup
```bash
  ./scripts/create-test-cluster.sh
  ./monitoring-portforward.sh
```
Test scenarios
```bash
  ./bin/knavigator -workflow resources/benchmarks/backfill/kueue/test1-hpc-backfill/run-test.yaml
```