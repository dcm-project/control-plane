package service_test

import (
	"context"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/config"
	"github.com/dcm-project/control-plane/internal/catalog/service"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
	"github.com/dcm-project/control-plane/internal/catalog/testutil"
)

func cpuSpec(minCPU, maxCPU any) map[string]any {
	return map[string]any{
		"resources": map[string]any{
			"cpu": map[string]any{"min": minCPU, "max": maxCPU},
		},
	}
}

var _ = Describe("Resolved spec CPU validation", func() {
	DescribeTable("rejects invalid cpu quantities",
		func(minCPU, maxCPU any, expected error) {
			err := service.ValidateResolvedSpec(cpuSpec(minCPU, maxCPU))
			Expect(err).To(MatchError(expected))
			Expect(err.Error()).To(ContainSubstring("resources.cpu"))
		},
		Entry("malformed min", "invalid", "1000m", service.ErrInvalidCPUQuantity),
		Entry("malformed max", "1000m", "invalid", service.ErrInvalidCPUQuantity),
		Entry("zero millicores", "0m", "1000m", service.ErrInvalidCPUQuantity),
		Entry("zero cores", "0", "1", service.ErrInvalidCPUQuantity),
		Entry("fractional cores", "0.5", "1", service.ErrInvalidCPUQuantity),
		Entry("leading zero", "0500m", "1000m", service.ErrInvalidCPUQuantity),
		Entry("negative", "-500m", "1000m", service.ErrInvalidCPUQuantity),
		Entry("unknown unit", "500Mi", "1000m", service.ErrInvalidCPUQuantity),
		Entry("whitespace", " 500m", "1000m", service.ErrInvalidCPUQuantity),
		Entry("out of range cores", "99999999999999999999", "1", service.ErrInvalidCPUQuantity),
		Entry("non-string value", float64(0.5), "1000m", service.ErrInvalidCPUQuantity),
		Entry("explicit null min", nil, "1000m", service.ErrInvalidCPUQuantity),
		Entry("explicit null max", "500m", nil, service.ErrInvalidCPUQuantity),
		Entry("malformed CEL reference", "${db.cpu_min", "1000m", service.ErrInvalidCELExpression),
		Entry("min greater than max in millicores", "1000m", "500m", service.ErrCPUMinGreaterThanMax),
		Entry("min greater than max across units", "2", "1000m", service.ErrCPUMinGreaterThanMax),
	)

	DescribeTable("accepts valid cpu quantities",
		func(minCPU, maxCPU any) {
			Expect(service.ValidateResolvedSpec(cpuSpec(minCPU, maxCPU))).To(Succeed())
		},
		Entry("millicores", "500m", "1000m"),
		Entry("whole cores", "1", "2"),
		Entry("mixed units", "500m", "1"),
		Entry("equal values", "1000m", "1"),
		Entry("unset template values", "", ""),
		Entry("CEL references bound at apply time", "${db.cpu_min}", "${db.cpu_max}"),
	)

	It("accepts a cpu object with no min/max keys", func() {
		spec := map[string]any{"resources": map[string]any{"cpu": map[string]any{}}}
		Expect(service.ValidateResolvedSpec(spec)).To(Succeed())
	})

	It("validates cpu objects nested in arrays", func() {
		spec := map[string]any{
			"nodes": []any{
				map[string]any{"cpu": map[string]any{"min": "500m", "max": "1000m"}},
				map[string]any{"cpu": map[string]any{"min": "2000m", "max": "1000m"}},
			},
		}
		err := service.ValidateResolvedSpec(spec)
		Expect(err).To(MatchError(service.ErrCPUMinGreaterThanMax))
		Expect(err.Error()).To(ContainSubstring("nodes[1].cpu"))
	})

	It("ignores cpu fields that are not min/max objects", func() {
		spec := map[string]any{
			"nodes": map[string]any{
				"control_plane": map[string]any{"cpu": float64(4)},
			},
		}
		Expect(service.ValidateResolvedSpec(spec)).To(Succeed())
	})

	It("ignores cpu objects under provider_hints", func() {
		spec := map[string]any{
			"resources": map[string]any{
				"cpu": map[string]any{"min": "500m", "max": "1000m"},
			},
			"provider_hints": map[string]any{
				"kubernetes": map[string]any{
					"cpu": map[string]any{"min": float64(1), "max": float64(2)},
				},
			},
		}
		Expect(service.ValidateResolvedSpec(spec)).To(Succeed())
	})
})

var _ = Describe("CatalogItemInstance Create with container CPU values", func() {
	var (
		ctx context.Context
		db  *gorm.DB
		str store.Store
		svc service.Service
		pm  *mockPMClient
	)

	const catalogItemID = "ci-container-cpu"

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
		Expect(err).ToNot(HaveOccurred())
		Expect(db.Exec("PRAGMA foreign_keys = ON").Error).To(Succeed())
		Expect(db.AutoMigrate(&model.ServiceType{}, &model.CatalogItem{}, &model.CatalogItemInstance{})).To(Succeed())
		str = store.NewStore(db, slog.Default())
		pm = &mockPMClient{}
		svc, err = service.NewService(str, pm, config.DefaultSeedConfig(), slog.Default())
		Expect(err).ToNot(HaveOccurred())

		// Mirrors the seeded container service type: an empty resources template
		// the catalog item and user values fill in.
		ensureServiceTypeWithSpec(ctx, str, "container-cpu", "container-cpu", map[string]any{
			"image":     map[string]any{"reference": ""},
			"resources": map[string]any{"cpu": map[string]any{"min": "", "max": ""}},
		})
		// Editable CPU fields with no validation_schema: the catalog item itself
		// places no constraint on the values.
		ensureCatalogItemWithFields(ctx, str, catalogItemID, "container-cpu", []model.FieldConfiguration{
			{Path: "image.reference", Default: "quay.io/dcm/app:v1", Editable: false},
			{Path: "resources.cpu.min", Default: "500m", Editable: true},
			{Path: "resources.cpu.max", Default: "1000m", Editable: true},
			{Path: "resources.cpu", Editable: true},
		})
	})

	AfterEach(func() {
		if str != nil {
			Expect(str.Close()).To(Succeed())
		}
	})

	createWithCPU := func(minCPU, maxCPU string) error {
		_, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
			ApiVersion:  "v1alpha1",
			DisplayName: "CPU Instance",
			Spec: v1alpha1.CatalogItemInstanceSpec{
				CatalogItemId: catalogItemID,
				UserValues: []v1alpha1.UserValue{
					{Resource: testutil.DefaultResourceName, Path: "resources.cpu.min", Value: minCPU},
					{Resource: testutil.DefaultResourceName, Path: "resources.cpu.max", Value: maxCPU},
				},
			},
		})
		return err
	}

	DescribeTable("rejects the request before dispatching to placement",
		func(minCPU, maxCPU string, expected error) {
			Expect(createWithCPU(minCPU, maxCPU)).To(MatchError(expected))
			Expect(pm.createCalls).To(Equal(0))
		},
		Entry(`min="invalid", max="1000m"`, "invalid", "1000m", service.ErrInvalidCPUQuantity),
		Entry(`min="1000m", max="500m"`, "1000m", "500m", service.ErrCPUMinGreaterThanMax),
		Entry(`min="0m", max="1000m"`, "0m", "1000m", service.ErrInvalidCPUQuantity),
		Entry(`min="0.5", max="1"`, "0.5", "1", service.ErrInvalidCPUQuantity),
	)

	// An object-valued override replaces the whole cpu object at once, so the
	// per-field CEL and schema checks never see its min/max strings.
	createWithCPUObject := func(cpu any) error {
		_, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
			ApiVersion:  "v1alpha1",
			DisplayName: "CPU Instance",
			Spec: v1alpha1.CatalogItemInstanceSpec{
				CatalogItemId: catalogItemID,
				UserValues: []v1alpha1.UserValue{
					{Resource: testutil.DefaultResourceName, Path: "resources.cpu", Value: cpu},
				},
			},
		})
		return err
	}

	DescribeTable("rejects an object-valued resources.cpu override",
		func(cpu any, expected error) {
			Expect(createWithCPUObject(cpu)).To(MatchError(expected))
			Expect(pm.createCalls).To(Equal(0))
		},
		Entry("malformed CEL reference", map[string]any{"min": "${bad", "max": "1000m"}, service.ErrInvalidCELExpression),
		Entry("null min", map[string]any{"min": nil, "max": "1000m"}, service.ErrInvalidCPUQuantity),
		Entry("min greater than max", map[string]any{"min": "2", "max": "1000m"}, service.ErrCPUMinGreaterThanMax),
	)

	It("creates the instance for valid CPU values", func() {
		Expect(createWithCPU("500m", "1000m")).To(Succeed())
		Expect(pm.createCalls).To(Equal(1))
	})
})
