# Helm charts for Knavigator

To configure virtual nodes, use the provided Helm chart to deploy [Knavigator](https://github.com/nvidia/knavigator/).

#### Install Helm charts

First, install Helm v3 using the official script:

```console
curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 && \
    chmod 700 get_helm.sh && \
    ./get_helm.sh
```
Next, setup the Helm repo:

```console
helm repo add knavigator \
    https://nvidia.github.io/knavigator/helm-charts
```
Update the repo:

```console
helm repo update
```

Install the official chart for knavigator:

```console
helm install \
    --generate-name \
    knavigator/virtual-nodes
```
