Cluster setup
```bash
  ./scripts/create-test-cluster.sh
  ./monitoring-portforward.sh
```
Cleanup commands
```bash
  kubectl -n default delete job --all
  kubectl -n default delete localqueue --all
  kubectl -n default delete clusterqueue --all
  kubectl -n default delete resourceflavor --all
  helm upgrade --install virtual-nodes charts/virtual-nodes -f resources/benchmarks/backfill/templates/nodes/nodes-cleanup.yaml
```
Test scenarios
```bash
  ./bin/knavigator -workflow resources/benchmarks/backfill/kueue/test2-backfill-performance/run-test-10x100.yaml
  ./bin/knavigator -workflow resources/benchmarks/backfill/kueue/test2-backfill-performance/run-test-10x100-multiple-queues.yaml
  ./bin/knavigator -workflow resources/benchmarks/backfill/kueue/test2-backfill-performance/run-test-35x100.yaml
  ./bin/knavigator -workflow resources/benchmarks/backfill/kueue/test2-backfill-performance/run-test-35x100-multiple-queues.yaml
  ./bin/knavigator -workflow resources/benchmarks/backfill/kueue/test2-backfill-performance/run-test-100x100.yaml
  ./bin/knavigator -workflow resources/benchmarks/backfill/kueue/test2-backfill-performance/run-test-100x100-multiple-queues.yaml
```