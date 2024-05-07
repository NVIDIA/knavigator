# Deployment

Knavigator manages tasks within a Kubernetes cluster by interacting with the control plane and utilizing the scheduling framework. The cluster may consist of either physical or simulated nodes. To handle these simulated nodes, Knavigator employs KWOK.

## Deploying Prometheus

Knavigator operates alongside the Prometheus node-resource-exporter. Additionally, the scheduling frameworks produce custom metrics. To utilize these features effectively, the cluster should have a Prometheus server installed.

```bash
helm repo add --force-update prometheus-community \
  https://prometheus-community.github.io/helm-charts

helm install kube-prometheus-stack \
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

## KWOK Integration

Knavigator integrates with KWOK to simulate large clusters with hundreds or thousands of virtual nodes. This allows for the execution of experiments in a resource-efficient manner, without the need to run actual user workloads. The integration is facilitated through the API Server, which communicates with KWOK to manage the virtual nodes.

To deploy the KWOK controller and the stages on a Kubernetes cluster, follow the instructions at [KWOK Installation Guide](https://kwok.sigs.k8s.io/docs/user/kwok-in-cluster).

```bash
KWOK_REPO=kubernetes-sigs/kwok
KWOK_LATEST_RELEASE="v0.5.2"

kubectl apply -f "https://github.com/${KWOK_REPO}/releases/download/${KWOK_LATEST_RELEASE}/kwok.yaml"

kubectl apply -f "https://github.com/${KWOK_REPO}/releases/download/${KWOK_LATEST_RELEASE}/stage-fast.yaml"

kubectl apply -f https://github.com/${KWOK_REPO}/raw/main/kustomize/stage/pod/chaos/pod-init-container-running-failed.yaml
kubectl apply -f https://github.com/${KWOK_REPO}/raw/main/kustomize/stage/pod/chaos/pod-container-running-failed.yaml
```

For configuring virtual nodes, you need to provide the `values.yaml` file to define the type and quantity of nodes you wish to create. You also have the option to enhance node configurations by adding annotations, labels, and conditions. For guidance, refer to the [values-example.yaml](../charts/virtual-nodes/values-example.yaml) file.

Currently, the system supports the following node types:
- [dgxa100.40g](https://docs.nvidia.com/dgx/dgxa100-user-guide/introduction-to-dgxa100.html#hardware-overview)
- [dgxa100.80g](https://docs.nvidia.com/dgx/dgxa100-user-guide/introduction-to-dgxa100.html#hardware-overview)
- [dgxh100.80g](https://docs.nvidia.com/dgx/dgxh100-user-guide/introduction-to-dgxh100.html#hardware-overview)
- cpu.x86

If you need to introduce additional node types, update the `values.yaml` file with the necessary node information (such as type and count) and include a parameters section in the [nodes.yaml](../charts/virtual-nodes/templates/nodes.yaml) file.

To deploy these nodes, use the Helm command:
```bash
helm install virtual-nodes charts/virtual-nodes -f charts/virtual-nodes/values.yaml
```

## Running Knavigator

Knavigator can be deployed inside a Kubernetes cluster or used externally from outside the cluster.

To use Knavigator outside the cluster, run
```bash
./bin/knavigator -tasks <task config>
```
In this mode, Knavigator requires the `KUBECONFIG` environment variable or the presence of the `-kubeconfig` or `-kubectx` command-line arguments.

To deploy Knavigator inside the cluster, you would need to create a pod. 
Details: TBD
