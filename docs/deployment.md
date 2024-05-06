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

To install the KWOK controller on a Kubernetes cluster, please follow [there instructions](https://kwok.sigs.k8s.io/docs/user/kwok-in-cluster/):

```bash
KWOK_REPO=kubernetes-sigs/kwok
KWOK_LATEST_RELEASE="v0.5.2"

kubectl apply -f "https://github.com/${KWOK_REPO}/releases/download/${KWOK_LATEST_RELEASE}/kwok.yaml"
kubectl apply -f "https://github.com/${KWOK_REPO}/releases/download/${KWOK_LATEST_RELEASE}/stage-fast.yaml"


```

Once installed, you'll be able to create virtual nodes. For example:

```bash
helm install virtual-nodes charts/virtual-nodes \
  --set-json nodes='[{"type":"dgxa100.40g","count":"4"},{"type":"dgxh100.80g","count":"2"}]'
```

Refer to the [node.yaml](../charts/virtual-nodes/templates/node.yaml) template to view the specifications of nodes currently supported.
To add more node specifications, simply follow the example of `node.yaml`.

## Running Knavigator

Knavigator can be deployed inside a Kubernetes cluster or used externally from outside the cluster.

To use Knavigator outside the cluster, run
```bash
./bin/knavigator -tasks <task config>
```
In this mode, Knavigator requires the `KUBECONFIG` environment variable or the presence of the `-kubeconfig` or `-kubectx` command-line arguments.

To deploy Knavigator inside the cluster, you would need to create a pod. 
Details: TBD
