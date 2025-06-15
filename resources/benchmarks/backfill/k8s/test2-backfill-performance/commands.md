
Cleanup commands
```bash
  kubectl -n default delete job --all
  helm upgrade --install virtual-nodes charts/virtual-nodes -f resources/benchmarks/backfill/templates/nodes/nodes-cleanup.yaml
```

Test scenarios
```bash
  ./bin/knavigator -workflow resources/benchmarks/backfill/k8s/test2-backfill-performance/run-test-10x100.yaml
  ./bin/knavigator -workflow resources/benchmarks/backfill/k8s/test2-backfill-performance/run-test-35x100.yaml
  ./bin/knavigator -workflow resources/benchmarks/backfill/k8s/test2-backfill-performance/run-test-100x100.yaml
```