## Example of running `YuniKorn` with `knavigator`

Install `YuniKorn` by following these [instructions](https://yunikorn.apache.org/docs/):

```bash
helm repo add yunikorn https://apache.github.io/yunikorn-release
helm repo update
helm install yunikorn yunikorn/yunikorn \
  --namespace yunikorn --create-namespace \
  --set-json nodeSelector='{"node-role.kubernetes.io/control-plane": ""}' \
  --set-json admissionController.nodeSelector='{"node-role.kubernetes.io/control-plane": ""}'
```

Run a YuniKorn job: 
```bash
./bin/knavigator -workflow resources/workflows/yunikorn/test-job.yml -cleanup
```

Run a preemption workflow with YuniKorn: 
```bash
./bin/knavigator -workflow resources/workflows/yunikorn/test-preemption.yml -cleanup
```
