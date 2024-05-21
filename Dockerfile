FROM ubuntu

RUN apt-get update && apt-get -y install curl vim
RUN curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 && chmod 700 get_helm.sh && ./get_helm.sh

WORKDIR /knavigator

COPY bin/* /usr/local/bin/
COPY resources /knavigator/resources
