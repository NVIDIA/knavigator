# Deployment

Knavigator manages tasks within a Kubernetes cluster by interacting with the control plane and utilizing the scheduling framework. The cluster may consist of either physical or simulated nodes. To handle these simulated nodes, Knavigator employs KWOK.

## Deploying Prometheus

Knavigator operates alongside the Prometheus node-resource-exporter. Additionally, the scheduling frameworks produce custom metrics. To utilize these features effectively, the cluster should have a Prometheus server installed.

```bash
helm repo add --force-update prometheus-community \
  https://prometheus-community.github.io/helm-charts

helm install -n prometheus --create-namespace kube-prometheus-stack \
  prometheus-community/kube-prometheus-stack \
  --set alertmanager.enabled=false \
  --set defaultRules.rules.alertmanager=false \
  --set defaultRules.rules.nodeExporterAlerting=false \
  --set defaultRules.rules.nodeExporterRecording=false \
  --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
  --set prometheus.prometheusSpec.podMonitorSelectorNilUsesHelmValues=false
```

## Setting up scheduling framework

Knavigator is compatible with any scheduling framework that operates within Kubernetes, including [Volcano](https://volcano.sh), [Kueue](https://kueue.sigs.k8s.io/), [Run.ai](https://www.run.ai/), and others. To deploy your chosen scheduling framework, please visit its respective website and follow the instructions there.

Some of the tested frameworks are: 
- [Volcano](https://volcano.sh/en/docs/installation/)
- [Jobset](https://github.com/kubernetes-sigs/jobset?tab=readme-ov-file#installation)
- [Kueue](https://kueue.sigs.k8s.io/docs/installation/)

## KWOK integration

Knavigator integrates with KWOK to simulate large clusters with hundreds or thousands of virtual nodes. This allows for the execution of experiments in a resource-efficient manner, without the need to run actual user workloads. The integration is facilitated through the API Server, which communicates with KWOK to manage the virtual nodes.

To deploy the KWOK controller and the stages on a Kubernetes cluster, follow the instructions at [KWOK Installation Guide](https://kwok.sigs.k8s.io/docs/user/kwok-in-cluster).

```bash
KWOK_REPO=kubernetes-sigs/kwok
KWOK_LATEST_RELEASE="v0.5.2"

kubectl apply -f "https://github.com/${KWOK_REPO}/releases/download/${KWOK_LATEST_RELEASE}/kwok.yaml"
```

Next, deploy and adjust the stages.
```bash
kubectl apply -f "https://github.com/${KWOK_REPO}/releases/download/${KWOK_LATEST_RELEASE}/stage-fast.yaml"

kubectl apply -f https://github.com/${KWOK_REPO}/raw/main/kustomize/stage/pod/chaos/pod-init-container-running-failed.yaml
kubectl apply -f https://github.com/${KWOK_REPO}/raw/main/kustomize/stage/pod/chaos/pod-container-running-failed.yaml

kubectl apply -f charts/overrides/kwok/pod-complete.yml
```

## Setting up virtual nodes

Virtual nodes are configured by setting the following node attributes: `type`, `count`, `annotations`, `labels`, `resources`, and `conditions`. The `type` and `count` attributes are mandatory, while the rest are optional.

There are three pre-defined node types:
- [dgxa100.40g](https://docs.nvidia.com/dgx/dgxa100-user-guide/introduction-to-dgxa100.html#hardware-overview)
- [dgxa100.80g](https://docs.nvidia.com/dgx/dgxa100-user-guide/introduction-to-dgxa100.html#hardware-overview)
- [dgxh100.80g](https://docs.nvidia.com/dgx/dgxh100-user-guide/introduction-to-dgxh100.html#hardware-overview)

For these types, the resource attributes are already configured, but you can still modify `count`, `annotations`, `labels`, and `conditions`. For example:
```yaml
- type: dgxa100.80g
  count: 2
  annotations: {}
  labels:
    nvidia.com/gpu.count: "8"
    nvidia.com/gpu.product: NVIDIA-A100-SXM4-80GB
  conditions:
  - message: kernel has no deadlock
    reason: KernelHasNoDeadlock
    status: "False"
    type: KernelDeadlock
```

For other node types, it is recommended to provide resource capacity. For example:
```yaml
- type: cpu.x86
  count: 2
  resources:
    hugepages-1Gi: 0
    hugepages-2Mi: 0
    pods: 110
    cpu: 48
    memory: 196692052Ki
    ephemeral-storage: 2537570228Ki
```

There are two ways to set up virtual nodes in the cluster, both of which require [Helm v3](https://helm.sh/docs/intro/install/) to be installed on your machine.

- Using the `helm` command:

  Run the `helm install` command and provide the `values.yaml` file that specifies the types and quantities of nodes you wish to create. For example, see the [values-example.yaml](../charts/virtual-nodes/values-example.yaml) file.
  
  To deploy the nodes defined in `values-example.yaml`, use the following command:
  ```bash
  helm upgrade --install virtual-nodes charts/virtual-nodes -f charts/virtual-nodes/values-example.yaml
  ```

- Using the task specification:

  Set up virtual nodes within the `Configure` task in the workflow config file.
  
  For this example, refer to [test-custom-resource.yml](../resources/workflows/test-custom-resource.yml#L11-L19).

> :warning: **Warning:** Ensure you deploy virtual nodes as the final step before launching `knavigator`. If you deploy any components after virtual nodes are created, the pods for these components might be assigned to virtual nodes, which could will their functionality.

## Running Knavigator

Knavigator can be deployed inside a Kubernetes cluster or used externally from outside the cluster.

### Running Knavigator outside the cluster

To use Knavigator outside the cluster, run
```bash
./bin/knavigator -workflow <workflow>
```

Additionally, you can use the `-cleanup` flag to remove any leftover objects created by the test, and the `-v` flag to increase verbosity. For usage instructions, use the `-h` flag.

For example,
```bash
./bin/knavigator -workflow resources/workflows/k8s/test-job.yml -v 4 -cleanup
```

In this mode, Knavigator requires the `KUBECONFIG` environment variable or the presence of the `-kubeconfig` or `-kubectx` command-line arguments.

### Running Knavigator inside the cluster

To deploy Knavigator inside the cluster, follow these steps:

- Create a service account and bind it to the `cluster-admin` role. Refer to [rbac.yml](examples/knavigator/rbac.yml) for an example.

- Deploy a pod or job that uses the service account and executes the Knavigator command, for example [test-job.yml](examples/knavigator/test-job.yml).

```bash
kubectl apply -f docs/examples/knavigator/rbac.yaml

kubectl apply -f docs/examples/knavigator/test-job.yml
```
