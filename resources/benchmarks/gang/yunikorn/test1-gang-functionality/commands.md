Cluster setup
```bash
  ./scripts/create-test-cluster.sh
  ./monitoring-portforward.sh
```
Cleanup commands
```bash
  kubectl -n default delete job --all
  helm upgrade --install virtual-nodes charts/virtual-nodes -f resources/benchmarks/gang/templates/nodes/nodes-cleanup.yaml
  kubectl -n yunikorn delete pod --all
```
Test scenarios
```bash
  ./bin/knavigator -workflow resources/benchmarks/gang/yunikorn/test1-gang-functionality/run-test-standard.yaml
  ./bin/knavigator -workflow resources/benchmarks/gang/yunikorn/test1-gang-functionality/run-test-standard-blocking-job.yaml
```