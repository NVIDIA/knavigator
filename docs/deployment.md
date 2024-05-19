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

There are two ways to set up virtual nodes in the cluster, both of which require [Helm v3](https://helm.sh/docs/intro/install/) to be installed on your machine.

### 1. Using the `helm` command

Run the `helm install` command and provide the `values.yaml` file that specifies the types and quantities of nodes you wish to create. For example, see the [values-example.yaml](../charts/virtual-nodes/values-example.yaml) file.
Currently, the system includes the following node types:
- [dgxa100.40g](https://docs.nvidia.com/dgx/dgxa100-user-guide/introduction-to-dgxa100.html#hardware-overview)
- [dgxa100.80g](https://docs.nvidia.com/dgx/dgxa100-user-guide/introduction-to-dgxa100.html#hardware-overview)
- [dgxh100.80g](https://docs.nvidia.com/dgx/dgxh100-user-guide/introduction-to-dgxh100.html#hardware-overview)
- cpu.x86

To deploy the nodes defined in `values-example.yaml`, use the following command:
```bash
helm upgrade --install virtual-nodes charts/virtual-nodes -f charts/virtual-nodes/values-example.yaml
```

### 2. Using the Task Specification

Set up virtual nodes within the `Configure` task in the task specification file. For this example, refer to [test-custom-resource.yml](../resources/tests/test-custom-resource.yml#L11-L19).

### Enhancing Node Configurations

In both methods, you can enhance node configurations by adding annotations, labels, and conditions.

To introduce additional node types, update the `values.yaml` file or the `Configure` task used for node configuration with the node information (such as type, count, etc.), and include a parameters section in the [nodes.yaml](../charts/virtual-nodes/templates/nodes.yaml) file.

> :warning: **Warning:** Ensure you deploy virtual nodes as the final step before launching `knavigator`. If you deploy any components after virtual nodes are created, the pods for these components might be assigned to virtual nodes, which could will their functionality.

## Running Knavigator

Knavigator can be deployed inside a Kubernetes cluster or used externally from outside the cluster.

To use Knavigator outside the cluster, run
```bash
./bin/knavigator -tasks <task config>
```
In this mode, Knavigator requires the `KUBECONFIG` environment variable or the presence of the `-kubeconfig` or `-kubectx` command-line arguments.

To deploy Knavigator inside the cluster, you would need to create a pod. 
Details: TBD
