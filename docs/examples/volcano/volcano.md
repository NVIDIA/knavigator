## Example of running Volcano `job` with `knavigator`

Install [volcano](https://volcano.sh):

Using YAML files:
```shell
kubectl apply -f https://raw.githubusercontent.com/volcano-sh/volcano/master/installer/volcano-development.yaml
```

Using helm:
```shell
helm repo add volcano-sh https://volcano-sh.github.io/helm-charts
helm repo update
helm install volcano volcano-sh/volcano -n volcano-system --create-namespace
```

Optionally, create a priority class:
```shell
kubectl create priorityclass normal-priority --value=100000
```

Run a Volcano job:
```shell
./bin/knavigator -workflow ./resources/workflows/volcano/test-job.yml
```
