# Policy domain (codegen and subsystem tests).
POLICY_DOMAIN := policy
POLICY_API := api/$(POLICY_DOMAIN)/v1alpha1
POLICY_ENGINE_API := api/$(POLICY_DOMAIN)/v1alpha1/engine
POLICY_SERVER_DIR := internal/$(POLICY_DOMAIN)/api/server
POLICY_ENGINE_SERVER_DIR := internal/$(POLICY_DOMAIN)/api/engine
POLICY_CLIENT_DIR := pkg/$(POLICY_DOMAIN)/client
POLICY_ENGINE_CLIENT_DIR := pkg/$(POLICY_DOMAIN)/engineclient

generate-policy-types:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(POLICY_API)/types.gen.cfg \
		-o $(POLICY_API)/types.gen.go \
		$(POLICY_API)/openapi.yaml

generate-policy-spec:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(POLICY_API)/spec.gen.cfg \
		-o $(POLICY_API)/spec.gen.go \
		$(POLICY_API)/openapi.yaml

generate-policy-server:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(POLICY_SERVER_DIR)/server.gen.cfg \
		-o $(POLICY_SERVER_DIR)/server.gen.go \
		$(POLICY_API)/openapi.yaml

generate-policy-client:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(POLICY_CLIENT_DIR)/client.gen.cfg \
		-o $(POLICY_CLIENT_DIR)/client.gen.go \
		$(POLICY_API)/openapi.yaml

generate-policy-crud-api: generate-policy-types generate-policy-spec generate-policy-server generate-policy-client

generate-policy-engine-types:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(POLICY_ENGINE_API)/types.gen.cfg \
		-o $(POLICY_ENGINE_API)/types.gen.go \
		$(POLICY_ENGINE_API)/openapi.yaml

generate-policy-engine-spec:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(POLICY_ENGINE_API)/spec.gen.cfg \
		-o $(POLICY_ENGINE_API)/spec.gen.go \
		$(POLICY_ENGINE_API)/openapi.yaml

generate-policy-engine-server:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(POLICY_ENGINE_SERVER_DIR)/server.gen.cfg \
		-o $(POLICY_ENGINE_SERVER_DIR)/server.gen.go \
		$(POLICY_ENGINE_API)/openapi.yaml

generate-policy-engine-client:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(POLICY_ENGINE_CLIENT_DIR)/client.gen.cfg \
		-o $(POLICY_ENGINE_CLIENT_DIR)/client.gen.go \
		$(POLICY_ENGINE_API)/openapi.yaml

generate-policy-engine-api: generate-policy-engine-types generate-policy-engine-spec generate-policy-engine-server generate-policy-engine-client

generate-policy-api: generate-policy-crud-api generate-policy-engine-api

check-policy-aep-api:
	spectral lint --fail-severity=warn ./$(POLICY_API)/openapi.yaml

check-policy-aep-engine:
	spectral lint --fail-severity=warn ./$(POLICY_ENGINE_API)/openapi.yaml

check-policy-aep: check-policy-aep-api check-policy-aep-engine

test-policy:
	$(GINKGO) $(GINKGO_FLAGS) ./internal/$(POLICY_DOMAIN)

policy-subsystem-test-up:
	$(COMPOSE) -f test/subsystem/$(POLICY_DOMAIN)/docker-compose.yaml up -d --build

policy-subsystem-test-down:
	$(COMPOSE) -f test/subsystem/$(POLICY_DOMAIN)/docker-compose.yaml down -v

policy-subsystem-test:
	$(GINKGO) $(GINKGO_FLAGS) -tags=subsystem ./test/subsystem/$(POLICY_DOMAIN)

.PHONY: generate-policy-types generate-policy-spec generate-policy-server \
	generate-policy-client generate-policy-crud-api generate-policy-engine-types generate-policy-engine-spec \
	generate-policy-engine-server generate-policy-engine-client generate-policy-engine-api generate-policy-api \
	check-policy-aep-api check-policy-aep-engine check-policy-aep test-policy \
	policy-subsystem-test-up policy-subsystem-test-down policy-subsystem-test
