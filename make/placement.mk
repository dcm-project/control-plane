# Placement domain (codegen and subsystem tests).
PLACEMENT_DOMAIN := placement
PLACEMENT_API := api/$(PLACEMENT_DOMAIN)/v1alpha1
PLACEMENT_SERVER_DIR := internal/$(PLACEMENT_DOMAIN)/api/server
PLACEMENT_CLIENT_DIR := pkg/$(PLACEMENT_DOMAIN)/client

generate-placement-types:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(PLACEMENT_API)/types.gen.cfg \
		-o $(PLACEMENT_API)/types.gen.go \
		$(PLACEMENT_API)/openapi.yaml

generate-placement-spec:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(PLACEMENT_API)/spec.gen.cfg \
		-o $(PLACEMENT_API)/spec.gen.go \
		$(PLACEMENT_API)/openapi.yaml

generate-placement-server:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(PLACEMENT_SERVER_DIR)/server.gen.cfg \
		-o $(PLACEMENT_SERVER_DIR)/server.gen.go \
		$(PLACEMENT_API)/openapi.yaml

generate-placement-client:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(PLACEMENT_CLIENT_DIR)/client.gen.cfg \
		-o $(PLACEMENT_CLIENT_DIR)/client.gen.go \
		$(PLACEMENT_API)/openapi.yaml

generate-placement-api: generate-placement-types generate-placement-spec generate-placement-server generate-placement-client

check-placement-aep:
	spectral lint --fail-severity=warn ./$(PLACEMENT_API)/openapi.yaml

test-placement:
	$(GINKGO) $(GINKGO_FLAGS) ./internal/$(PLACEMENT_DOMAIN)

placement-subsystem-test-up:
	$(COMPOSE) -f test/subsystem/$(PLACEMENT_DOMAIN)/docker-compose.yaml up -d --build

placement-subsystem-test-down:
	$(COMPOSE) -f test/subsystem/$(PLACEMENT_DOMAIN)/docker-compose.yaml down -v

placement-subsystem-test:
	$(GINKGO) $(GINKGO_FLAGS) -tags=subsystem ./test/subsystem/$(PLACEMENT_DOMAIN)

.PHONY: generate-placement-types generate-placement-spec \
	generate-placement-server generate-placement-client generate-placement-api check-placement-aep \
	test-placement placement-subsystem-test-up placement-subsystem-test-down placement-subsystem-test
