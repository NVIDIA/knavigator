FROM ubuntu

RUN apt-get update && apt-get -y install curl
RUN curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 && chmod 700 get_helm.sh && ./get_helm.sh

COPY bin/* /usr/local/bin/
COPY resources /etc/knavigator
