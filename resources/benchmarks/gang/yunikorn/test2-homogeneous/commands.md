
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
  ./bin/knavigator -workflow resources/benchmarks/gang/yunikorn/test2-homogeneous/run-test-small-cluster-10-pods.yaml
  ./bin/knavigator -workflow resources/benchmarks/gang/yunikorn/test2-homogeneous/run-test-small-cluster-100-pods.yaml
  ./bin/knavigator -workflow resources/benchmarks/gang/yunikorn/test2-homogeneous/run-test-big-cluster-10-pods.yaml
  ./bin/knavigator -workflow resources/benchmarks/gang/yunikorn/test2-homogeneous/run-test-big-cluster-100-pods.yaml
```