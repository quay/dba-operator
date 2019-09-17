#!/bin/bash

set -e
set -o pipefail
set -u

export MINIKUBE_VERSION=${MINIKUBE_VERSION:-v1.2.0}
export KUBERNETES_VERSION=${KUBERNETES_VERSION:-v1.15.2}
export KUBECONFIG=${KUBECONFIG:-$HOME/.kube/config}


kubectl > /dev/null || { curl -Lo kubectl https://storage.googleapis.com/kubernetes-release/release/$KUBERNETES_VERSION/bin/linux/amd64/kubectl && \
        chmod +x kubectl &&  \
        sudo mv kubectl /usr/local/bin/ ;}

minikube > /dev/null || { curl -Lo minikube https://storage.googleapis.com/minikube/releases/$MINIKUBE_VERSION/minikube-linux-amd64 && \
        chmod +x minikube && \
        sudo mv minikube /usr/local/bin/ ;}


minikube_create() {
    minikube version | awk '{ print $3 }' | grep -q "$MINIKUBE_VERSION" || { echo "Wrong minikube version"; exit 1; }

    pgrep -f "[m]inikube" >/dev/null || minikube start --cpus 2 --memory 2048 --kubernetes-version="$KUBERNETES_VERSION" || { echo 'Cannot start minikube.'; exit 1; }

    kubectl version --output json | jq .serverVersion.gitVersion | grep -q "$KUBERNETES_VERSION" && \
        kubectl version --output json | jq .clientVersion.gitVersion | grep -q "$KUBERNETES_VERSION" || { echo "Kubectl version does not match k8s version"; exit 1; }

    kubectl config use-context minikube
}

minikube_delete() {
    minikube delete
}

build_image() {
    eval "$(minikube docker-env)" || { echo 'Cannot switch to minikube docker'; exit 1; }
    make manager
    docker build -f Dockerfile.e2e -t quay.io/quay/dba-operator:local .
}


if [ $1 == "create-minikube" ]; then
    minikube_create
elif [ $1 == "delete-minikube" ]; then
    minikube_delete
elif [ $1 == "build-image" ]; then
    build_image
else
    echo "Specify create-minikube, delete-minikube, or build-image"
    exit 1
fi
