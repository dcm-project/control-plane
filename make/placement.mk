# Placement domain (unit tests only; no public HTTP API).
PLACEMENT_DOMAIN := placement

test-placement:
	$(GINKGO) $(GINKGO_FLAGS) ./internal/$(PLACEMENT_DOMAIN)

.PHONY: test-placement
