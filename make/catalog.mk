# Catalog manager (imported from catalog-manager repo layout).
CATALOG_DOMAIN := catalog
CATALOG_BINARY := catalog-manager
CATALOG_API := api/$(CATALOG_DOMAIN)/v1alpha1
CATALOG_SERVER_DIR := internal/$(CATALOG_DOMAIN)/api/server
CATALOG_CLIENT_DIR := pkg/$(CATALOG_DOMAIN)/client
CATALOG_SERVICETYPES_MODULE := github.com/dcm-project/control-plane/api/$(CATALOG_DOMAIN)/v1alpha1/servicetypes

build-catalog:
	go build -o bin/$(CATALOG_BINARY) ./cmd/$(CATALOG_BINARY)

run-catalog:
	DB_TYPE=sqlite DB_NAME=/tmp/catalog.db go run ./cmd/$(CATALOG_BINARY)

generate-catalog-types:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(CATALOG_API)/types.gen.cfg \
		-o $(CATALOG_API)/types.gen.go \
		$(CATALOG_API)/openapi.yaml

generate-catalog-spec:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(CATALOG_API)/spec.gen.cfg \
		-o $(CATALOG_API)/spec.gen.go \
		$(CATALOG_API)/openapi.yaml

generate-catalog-server:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(CATALOG_SERVER_DIR)/server.gen.cfg \
		-o $(CATALOG_SERVER_DIR)/server.gen.go \
		$(CATALOG_API)/openapi.yaml

generate-catalog-client:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(CATALOG_CLIENT_DIR)/client.gen.cfg \
		-o $(CATALOG_CLIENT_DIR)/client.gen.go \
		$(CATALOG_API)/openapi.yaml

generate-catalog-api: generate-catalog-types generate-catalog-spec generate-catalog-server generate-catalog-client generate-catalog-service-types

check-catalog-aep:
	spectral lint --fail-severity=warn ./$(CATALOG_API)/openapi.yaml

generate-catalog-service-types:
	@echo "Generating common types..."
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(CATALOG_API)/servicetypes/types.gen.cfg \
		-o $(CATALOG_API)/servicetypes/types.gen.go \
		$(CATALOG_API)/servicetypes/common.yaml
	@echo "Generating VM types..."
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(CATALOG_API)/servicetypes/vm/spec.gen.cfg \
		--import-mapping=../common.yaml:$(CATALOG_SERVICETYPES_MODULE) \
		-o $(CATALOG_API)/servicetypes/vm/types.gen.go \
		$(CATALOG_API)/servicetypes/vm/spec.yaml
	@echo "Generating Container types..."
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(CATALOG_API)/servicetypes/container/spec.gen.cfg \
		--import-mapping=../common.yaml:$(CATALOG_SERVICETYPES_MODULE) \
		-o $(CATALOG_API)/servicetypes/container/types.gen.go \
		$(CATALOG_API)/servicetypes/container/spec.yaml
	@echo "Generating Database types..."
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(CATALOG_API)/servicetypes/database/spec.gen.cfg \
		--import-mapping=../common.yaml:$(CATALOG_SERVICETYPES_MODULE) \
		-o $(CATALOG_API)/servicetypes/database/types.gen.go \
		$(CATALOG_API)/servicetypes/database/spec.yaml
	@echo "Generating Cluster types..."
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(CATALOG_API)/servicetypes/cluster/spec.gen.cfg \
		--import-mapping=../common.yaml:$(CATALOG_SERVICETYPES_MODULE) \
		-o $(CATALOG_API)/servicetypes/cluster/types.gen.go \
		$(CATALOG_API)/servicetypes/cluster/spec.yaml
	@echo "Generating Three-Tier App Demo types..."
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(CATALOG_API)/servicetypes/three_tier_app_demo/spec.gen.cfg \
		--import-mapping=../common.yaml:$(CATALOG_SERVICETYPES_MODULE) \
		-o $(CATALOG_API)/servicetypes/three_tier_app_demo/types.gen.go \
		$(CATALOG_API)/servicetypes/three_tier_app_demo/spec.yaml

catalog-subsystem-test-up:
	$(COMPOSE) -f test/subsystem/$(CATALOG_DOMAIN)/docker-compose.yaml up -d --build

catalog-subsystem-test-down:
	$(COMPOSE) -f test/subsystem/$(CATALOG_DOMAIN)/docker-compose.yaml down -v

catalog-subsystem-test:
	go run github.com/onsi/ginkgo/v2/ginkgo -r --randomize-all --fail-on-pending -tags=subsystem ./test/subsystem/$(CATALOG_DOMAIN)

.PHONY: build-catalog run-catalog generate-catalog-types generate-catalog-spec \
	generate-catalog-server generate-catalog-client generate-catalog-api check-catalog-aep \
	generate-catalog-service-types catalog-subsystem-test-up catalog-subsystem-test-down \
	catalog-subsystem-test
