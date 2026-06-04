# Service provider manager (imported from service-provider-manager repo layout).
SP_DOMAIN := sp
SP_BINARY := service-provider-manager
SP_PROVIDER_API := api/$(SP_DOMAIN)/v1alpha1/provider
SP_RM_API := api/$(SP_DOMAIN)/v1alpha1/resource_manager
SP_PROVIDER_SERVER_DIR := internal/$(SP_DOMAIN)/api/provider
SP_RM_SERVER_DIR := internal/$(SP_DOMAIN)/api/resource_manager
SP_PROVIDER_CLIENT_DIR := pkg/$(SP_DOMAIN)/client/provider
SP_RM_CLIENT_DIR := pkg/$(SP_DOMAIN)/client/resource_manager

build-sp:
	go build -o bin/$(SP_BINARY) ./cmd/$(SP_BINARY)

run-sp:
	DB_TYPE=sqlite DB_NAME=/tmp/sp.db go run ./cmd/$(SP_BINARY)

generate-sp-provider-types:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(SP_PROVIDER_API)/types.gen.cfg \
		-o $(SP_PROVIDER_API)/types.gen.go \
		$(SP_PROVIDER_API)/openapi.yaml

generate-sp-provider-spec:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(SP_PROVIDER_API)/spec.gen.cfg \
		-o $(SP_PROVIDER_API)/spec.gen.go \
		$(SP_PROVIDER_API)/openapi.yaml

generate-sp-provider-server:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(SP_PROVIDER_SERVER_DIR)/server.gen.cfg \
		-o $(SP_PROVIDER_SERVER_DIR)/server.gen.go \
		$(SP_PROVIDER_API)/openapi.yaml

generate-sp-provider-client:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(SP_PROVIDER_CLIENT_DIR)/client.gen.cfg \
		-o $(SP_PROVIDER_CLIENT_DIR)/client.gen.go \
		$(SP_PROVIDER_API)/openapi.yaml

generate-sp-provider-api: generate-sp-provider-types generate-sp-provider-spec generate-sp-provider-server generate-sp-provider-client

generate-sp-rm-types:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(SP_RM_API)/types.gen.cfg \
		-o $(SP_RM_API)/types.gen.go \
		$(SP_RM_API)/openapi.yaml

generate-sp-rm-spec:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(SP_RM_API)/spec.gen.cfg \
		-o $(SP_RM_API)/spec.gen.go \
		$(SP_RM_API)/openapi.yaml

generate-sp-rm-server:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(SP_RM_SERVER_DIR)/server.gen.cfg \
		-o $(SP_RM_SERVER_DIR)/server.gen.go \
		$(SP_RM_API)/openapi.yaml

generate-sp-rm-client:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(SP_RM_CLIENT_DIR)/client.gen.cfg \
		-o $(SP_RM_CLIENT_DIR)/client.gen.go \
		$(SP_RM_API)/openapi.yaml

generate-sp-rm-api: generate-sp-rm-types generate-sp-rm-spec generate-sp-rm-server generate-sp-rm-client

generate-sp-api: generate-sp-provider-api generate-sp-rm-api

check-sp-aep-provider:
	spectral lint --fail-severity=warn ./$(SP_PROVIDER_API)/openapi.yaml

check-sp-aep-rm:
	spectral lint --fail-severity=warn ./$(SP_RM_API)/openapi.yaml

check-sp-aep: check-sp-aep-provider check-sp-aep-rm

sp-subsystem-test-up:
	$(COMPOSE) -f test/subsystem/$(SP_DOMAIN)/docker-compose.yaml up -d --build

sp-subsystem-test-down:
	$(COMPOSE) -f test/subsystem/$(SP_DOMAIN)/docker-compose.yaml down -v

sp-subsystem-test:
	go run github.com/onsi/ginkgo/v2/ginkgo -r --randomize-all --fail-on-pending -tags=subsystem ./test/subsystem/$(SP_DOMAIN)

.PHONY: build-sp run-sp generate-sp-provider-types generate-sp-provider-spec generate-sp-provider-server \
	generate-sp-provider-client generate-sp-provider-api generate-sp-rm-types generate-sp-rm-spec \
	generate-sp-rm-server generate-sp-rm-client generate-sp-rm-api generate-sp-api \
	check-sp-aep-provider check-sp-aep-rm check-sp-aep \
	sp-subsystem-test-up sp-subsystem-test-down sp-subsystem-test
