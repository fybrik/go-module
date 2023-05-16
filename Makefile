DOCKER_NAME = go-module
DOCKER_HOSTNAME = ghcr.io
DOCKER_NAMESPACE = aradhalevy
DOCKER_TAG ?= master

TEMP := /tmp
CHART_LOCAL_PATH ?= helm 
CHART_NAME ?= go-module-chart
HELM_RELEASE ?= rel1-${DOCKER_NAME}
HELM_TAG ?= 0.0.0


IMG := ${DOCKER_HOSTNAME}/${DOCKER_NAMESPACE}/${DOCKER_NAME}:${DOCKER_TAG}


CHART_REGISTRY_PATH := oci://${DOCKER_HOSTNAME}/${DOCKER_NAMESPACE}

# To enable OCI experimental support for Helm versions prior to v3.8.0, HELM_EXPERIMENTAL_OCI is set
export HELM_EXPERIMENTAL_OCI=1
export GODEBUG=x509ignoreCN=0

.PHONY: helm-verify
helm-verify: 
	helm lint ${CHART_LOCAL_PATH}
	helm install --dry-run ${HELM_RELEASE} ${CHART_LOCAL_PATH} ${HELM_VALUES}

.PHONY: helm-uninstall
helm-uninstall: 
	helm uninstall ${HELM_RELEASE} || true

.PHONY: helm-install
helm-install: helm
	helm install ${HELM_RELEASE} ${CHART_LOCAL_PATH} ${HELM_VALUES}

.PHONY: helm-chart-push
helm-chart-push:
	helm package ${CHART_LOCAL_PATH} --version=${HELM_TAG} --destination=${TEMP}
	helm push ${TEMP}/${CHART_NAME}-${HELM_TAG}.tgz ${CHART_REGISTRY_PATH}
	rm -rf ${TEMP}/${CHART_NAME}-${HELM_TAG}.tgz

.PHONY: docker-build 
docker-build:
	docker build . -t ${IMG}

.PHONY: docker-push 
docker-push: docker-build
	docker push ${IMG}

