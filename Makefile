SHELL := /bin/bash
BIN := $(HOME)/.local/bin
BUF_VERSION := 1.73.0
K3D_VERSION := 5.9.0
TF_VERSION := 1.16.4
export PATH := $(BIN):$(HOME)/go/bin:$(PATH)

.PHONY: tools gen proto-lint up down smoke test k3d-up k3d-down helm-install images-k3d

tools:
	mkdir -p $(BIN)
	curl -sSfL https://github.com/bufbuild/buf/releases/download/v$(BUF_VERSION)/buf-Linux-x86_64 -o $(BIN)/buf && chmod +x $(BIN)/buf
	curl -sSfL https://github.com/k3d-io/k3d/releases/download/v$(K3D_VERSION)/k3d-linux-amd64 -o $(BIN)/k3d && chmod +x $(BIN)/k3d
	curl -sSfL https://releases.hashicorp.com/terraform/$(TF_VERSION)/terraform_$(TF_VERSION)_linux_amd64.zip -o /tmp/tf.zip && unzip -o -q /tmp/tf.zip terraform -d $(BIN) && rm /tmp/tf.zip
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install connectrpc.com/connect/cmd/protoc-gen-connect-go@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install github.com/pressly/goose/v3/cmd/goose@latest

gen:
	cd proto && buf generate
	cd services/ai-py && uv run python -m grpc_tools.protoc -I ../../proto \
	  --python_out=src/ai/gen --pyi_out=src/ai/gen \
	  ../../proto/compliance/v1/invoice.proto ../../proto/compliance/v1/validator.proto ../../proto/compliance/v1/events.proto ../../proto/compliance/v1/export.proto
	find services/ai-py/src/ai/gen -type d -exec touch {}/__init__.py \;
	sed -i 's/^from compliance\.v1 import/from ai.gen.compliance.v1 import/' services/ai-py/src/ai/gen/compliance/v1/*_pb2.py services/ai-py/src/ai/gen/compliance/v1/*_pb2.pyi

proto-lint:
	cd proto && buf lint && buf format -d --exit-code

up:
	docker compose -f deploy/compose/compose.yaml up -d --build --wait

down:
	docker compose -f deploy/compose/compose.yaml down -v

smoke:
	cd e2e && npx playwright test

test:
	cd services/validator-rs && cargo test
	cd services/api-go && go test ./...
	cd services/ai-py && uv run pytest -q

# Story 10: Helm umbrella chart on k3d (ADR 012). One k3d cluster or compose
# stack at a time (7 GB RAM rule) — never run both together.
k3d-up:
	k3d cluster create --config deploy/k3d/cluster.yaml
	helm repo add cnpg https://cloudnative-pg.github.io/charts
	helm repo update cnpg
	helm upgrade --install cnpg cnpg/cloudnative-pg -n cnpg-system --create-namespace --wait -f deploy/k3d/cnpg-values.yaml

k3d-down:
	k3d cluster delete --config deploy/k3d/cluster.yaml

# Builds the four service images plus the Zitadel bootstrap tools image, all
# tagged :dev (values.yaml sets image.tag: dev, pullPolicy: Never), and
# imports them into the running k3d cluster so nothing is pulled.
images-k3d:
	docker build -f services/api-go/Dockerfile -t api-go:dev .
	docker build -f services/validator-rs/Dockerfile -t validator-rs:dev .
	docker build -f services/ai-py/Dockerfile -t ai-py:dev .
	docker build -f apps/web/Dockerfile -t web:dev .
	docker build -t zitadel-bootstrap:dev deploy/k3d/bootstrap-tools
	k3d image import api-go:dev validator-rs:dev ai-py:dev web:dev zitadel-bootstrap:dev -c compliance

helm-install:
	helm dependency build deploy/helm/compliance
	helm upgrade --install compliance deploy/helm/compliance -n compliance --create-namespace --wait --timeout 10m
