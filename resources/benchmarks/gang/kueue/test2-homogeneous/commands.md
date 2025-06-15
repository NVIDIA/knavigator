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
  kubectl -n monitoring delete pod --all
```
Test scenarios
```bash
  ./bin/knavigator -workflow resources/benchmarks/gang/kueue/test2-homogeneous/run-test-small-cluster-10-pods.yaml
  ./bin/knavigator -workflow resources/benchmarks/gang/kueue/test2-homogeneous/run-test-small-cluster-100-pods.yaml
  ./bin/knavigator -workflow resources/benchmarks/gang/kueue/test2-homogeneous/run-test-big-cluster-10-pods.yaml
  ./bin/knavigator -workflow resources/benchmarks/gang/kueue/test2-homogeneous/run-test-big-cluster-100-pods.yaml
```