
Cluster setup
```bash
  ./scripts/create-test-cluster.sh
  ./monitoring-portforward.sh
```
Cleanup commands
```bash
  kubectl delete jobs.batch.volcano.sh --all
  helm upgrade --install virtual-nodes charts/virtual-nodes -f resources/benchmarks/gang/templates/nodes/nodes-cleanup.yaml
```
Test scenarios
```bash
  ./bin/knavigator -workflow resources/benchmarks/gang/volcano/test1-gang-functionality/run-test-standard.yaml
  ./bin/knavigator -workflow resources/benchmarks/gang/volcano/test1-gang-functionality/run-test-standard-blocking-job.yaml
```