//go:build subsystem

package subsystem_test

import (
	"context"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1alpha1 "github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/testutil"
	"github.com/google/uuid"
)

// Container CPU values are constrained by the service type contract
// (CpuResources in api/catalog/v1alpha1/servicetypes/common.yaml) even when the
// catalog item declares the fields editable without a validation_schema. They must
// be rejected synchronously, before the instance reaches the provider.
var _ = Describe("CatalogItemInstance container CPU validation", func() {
	var catalogItemID string

	BeforeEach(func() {
		resetWireMock()
		stubPMCreateResource()

		catalogItemID = "ci-cpu-" + uuid.NewString()[:8]
		editableTrue := true
		createTestCatalogItem(catalogItemID, "Container CPU Item", "container", []v1alpha1.FieldConfiguration{
			{
				Path:        "image.reference",
				DisplayName: stringPtr("Image"),
				Default:     "quay.io/dcm-project/app:latest",
			},
			{
				Path:        "resources.cpu.min",
				DisplayName: stringPtr("CPU min"),
				Editable:    &editableTrue,
				Default:     "500m",
			},
			{
				Path:        "resources.cpu.max",
				DisplayName: stringPtr("CPU max"),
				Editable:    &editableTrue,
				Default:     "1000m",
			},
		})
	})

	createWithCPU := func(minCPU, maxCPU string) int {
		instID := "inst-cpu-" + uuid.NewString()[:8]
		params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
		body := v1alpha1.CatalogItemInstance{
			ApiVersion:  "v1alpha1",
			DisplayName: "CPU Instance",
			Spec: v1alpha1.CatalogItemInstanceSpec{
				CatalogItemId: catalogItemID,
				UserValues: []v1alpha1.UserValue{
					{Resource: testutil.DefaultResourceName, Path: "resources.cpu.min", Value: minCPU},
					{Resource: testutil.DefaultResourceName, Path: "resources.cpu.max", Value: maxCPU},
				},
			},
		}
		resp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		return resp.StatusCode()
	}

	DescribeTable("returns 400 without dispatching to the placement manager",
		func(minCPU, maxCPU string) {
			Expect(createWithCPU(minCPU, maxCPU)).To(Equal(http.StatusBadRequest))
			verifyPMCreateResourceCalled(0)
		},
		Entry(`min="invalid", max="1000m"`, "invalid", "1000m"),
		Entry(`min="1000m", max="500m"`, "1000m", "500m"),
		Entry(`min="0m", max="1000m"`, "0m", "1000m"),
		Entry(`min="0.5", max="1"`, "0.5", "1"),
	)

	It("returns 201 for valid millicore values", func() {
		Expect(createWithCPU("500m", "1000m")).To(Equal(http.StatusCreated))
		verifyPMCreateResourceCalled(1)
	})
})
