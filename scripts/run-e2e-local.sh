#!/usr/bin/env bash
set -e
set -o pipefail
set -u
set -x


export REPO=${REPO:-quay.io/quay/dba-operator}
export TAG=${TAG:-$(git rev-parse --short HEAD)}

SCRIPTS_DIR=$(dirname "${BASH_SOURCE[0]}")


"${SCRIPTS_DIR}"/minikube.sh create-minikube

"${SCRIPTS_DIR}"/minikube.sh build-image

make install || true # Install the genereated CRDs manifest in the cluster
make test-e2e-local # Run the tests

"${SCRIPTS_DIR}"/minikube.sh delete-minikube
