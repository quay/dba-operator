REPO?=quay.io/quay/dba-operator
TAG?=$(shell git rev-parse --short HEAD)

# Image URL to use all building/pushing image targets
IMG ?= ${REPO}:${TAG}
DB_IMG ?= mysql/mysql-server:latest

# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: manager

# Run tests
test: generate fmt vet manifests
	go test ./api/... ./controllers/... -coverprofile cover.out

# Run go test: e2e tests
.PHONY: test-e2e-local
test-e2e-local: KUBECONFIG?=$(HOME)/.kube/config
test-e2e-local:
	go test ./test/e2e/... --kubeconfig=${KUBECONFIG} --operator-image=${REPO}:${TAG} --db-image=${DB_IMG}

# Run e2e tests: Setup/teardown minikube, build image and go test
.PHONY: run-e2e-local
run-e2e-local:
	REPO=${REPO} TAG=${TAG} ./scripts/run-e2e-local.sh

# Build manager binary
manager: generate fmt vet
	go build -o bin/manager main.go

installapi:
	kubectl apply -f deploy/databasemigration.yaml -f deploy/manageddatabase.yaml

devenv: installapi
	kubectl apply -f deploy/pushgateway.yaml
	kubectl apply -f deploy/examples/

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet
	go run ./main.go

# Install CRDs into a cluster
install: manifests
	kubectl apply -f config/crd/bases

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests
	kubectl apply -f config/crd/bases
	kustomize build config/default | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt paths=./api/...

# Build the docker image
docker-build: test
	docker build . -t ${IMG}
	@echo "updating kustomize image patch file for manager resource"
	sed -i'' -e 's@image: .*@image: '"${IMG}"'@' ./config/default/manager_image_patch.yaml

# Push the docker image
docker-push:
	docker push ${IMG}

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.2.0-beta.4
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif
