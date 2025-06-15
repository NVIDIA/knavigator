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
  kubectl -n default delete topology --all
  helm upgrade --install virtual-nodes charts/virtual-nodes -f resources/benchmarks/gang/templates/nodes/nodes-cleanup.yaml
```
Test scenarios
```bash
  ./bin/knavigator -workflow resources/benchmarks/gang/kueue/test1-gang-functionality/run-test-standard-TAS.yaml
  ./bin/knavigator -workflow resources/benchmarks/gang/kueue/test1-gang-functionality/run-test-standard-blocking-job-TAS.yaml
```