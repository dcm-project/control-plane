package service_test

import (
	"context"
	"errors"
	"time"

	"github.com/dcm-project/control-plane/api/policy/v1alpha1"
	"github.com/dcm-project/control-plane/internal/policy/opa"
	"github.com/dcm-project/control-plane/internal/policy/service"
	"github.com/dcm-project/control-plane/internal/policy/store"
	"github.com/dcm-project/control-plane/internal/policy/store/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func strPtr(s string) *string { return &s }

func policyTypePtr(t v1alpha1.PolicyPolicyType) *v1alpha1.PolicyPolicyType { return &t }

type rollbackMockEngine struct {
	compileFunc func(ctx context.Context, policies []opa.PolicyModule) error
}

func (m *rollbackMockEngine) Compile(ctx context.Context, policies []opa.PolicyModule) error {
	if m.compileFunc != nil {
		return m.compileFunc(ctx, policies)
	}
	return nil
}

func (m *rollbackMockEngine) ValidateRego(_ context.Context, _ string) error {
	return nil
}

func (m *rollbackMockEngine) EvaluatePolicy(_ context.Context, _ string, _ map[string]any) (*opa.EvaluationResult, error) {
	return &opa.EvaluationResult{Defined: false}, nil
}

var _ = Describe("PolicyService", func() {
	var (
		db            *gorm.DB
		dataStore     store.Store
		engine        opa.Engine
		policyService service.PolicyService
		ctx           context.Context
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&model.Policy{})).To(Succeed())

		dataStore = store.NewStore(db)

		// Create real embedded OPA engine
		engine = opa.NewEngine()

		policyService = service.NewPolicyService(dataStore, engine)
		ctx = context.Background()
	})

	AfterEach(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	Describe("CreatePolicy", func() {
		It("should create policy with client-specified ID", func() {
			clientID := "my-custom-policy"
			regoCode := "package test\ndefault allow = true"

			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    &regoCode,
			}

			created, err := policyService.CreatePolicy(ctx, policy, &clientID)

			Expect(err).ToNot(HaveOccurred())
			Expect(created).NotTo(BeNil())
			Expect(*created.Id).To(Equal("my-custom-policy"))
			Expect(*created.Path).To(Equal("policies/my-custom-policy"))
			Expect(created.DisplayName).NotTo(BeNil())
			Expect(*created.DisplayName).To(Equal("Test Policy"))
			Expect(created.PolicyType).NotTo(BeNil())
			Expect(*created.PolicyType).To(Equal(v1alpha1.GLOBAL))
			Expect(created.RegoCode).NotTo(BeNil())
			Expect(*created.RegoCode).To(Equal(regoCode))
			Expect(*created.Enabled).To(BeTrue())           // Default value
			Expect(*created.Priority).To(Equal(int32(500))) // Default value
		})

		It("should create policy with server-generated UUID", func() {
			regoCode := "package test\ndefault allow = true"

			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.USER),
				RegoCode:    &regoCode,
			}

			created, err := policyService.CreatePolicy(ctx, policy, nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(created).NotTo(BeNil())
			Expect(*created.Id).NotTo(BeEmpty())
			Expect(*created.Path).To(HavePrefix("policies/"))
			// UUID format validation
			Expect(*created.Id).To(MatchRegexp(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`))
		})

		It("should validate RegoCode is non-empty", func() {
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr(""),
			}

			_, err := policyService.CreatePolicy(ctx, policy, nil)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
			Expect(serviceErr.Message).To(ContainSubstring("rego_code is required"))
		})

		It("should validate priority is at least 1", func() {
			priority := int32(0)
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
				Priority:    &priority,
			}

			_, err := policyService.CreatePolicy(ctx, policy, nil)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
			Expect(serviceErr.Message).To(ContainSubstring("priority must be between 1 and 1000"))
		})

		It("should reject negative priority", func() {
			priority := int32(-1)
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
				Priority:    &priority,
			}

			_, err := policyService.CreatePolicy(ctx, policy, nil)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
			Expect(serviceErr.Message).To(ContainSubstring("priority must be between 1 and 1000"))
		})

		It("should accept priority at minimum (1)", func() {
			priority := int32(1)
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy Min Priority"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
				Priority:    &priority,
			}

			created, err := policyService.CreatePolicy(ctx, policy, nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(created).NotTo(BeNil())
			Expect(*created.Priority).To(Equal(int32(1)))
		})

		It("should accept priority at maximum (1000)", func() {
			priority := int32(1000)
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy Max Priority"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
				Priority:    &priority,
			}

			created, err := policyService.CreatePolicy(ctx, policy, nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(created).NotTo(BeNil())
			Expect(*created.Priority).To(Equal(int32(1000)))
		})

		It("should reject priority above maximum (1001)", func() {
			priority := int32(1001)
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
				Priority:    &priority,
			}

			_, err := policyService.CreatePolicy(ctx, policy, nil)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
			Expect(serviceErr.Message).To(ContainSubstring("priority must be between 1 and 1000"))
		})

		It("should accept priority in mid-range (500)", func() {
			priority := int32(500)
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy Mid Priority"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
				Priority:    &priority,
			}

			created, err := policyService.CreatePolicy(ctx, policy, nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(created).NotTo(BeNil())
			Expect(*created.Priority).To(Equal(int32(500)))
		})

		It("should validate RegoCode is not just whitespace", func() {
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("   \n\t  "),
			}

			_, err := policyService.CreatePolicy(ctx, policy, nil)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
		})

		It("should validate ID format per AEP-122", func() {
			invalidID := "Invalid-ID-With-CAPS"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}

			_, err := policyService.CreatePolicy(ctx, policy, &invalidID)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
			Expect(serviceErr.Message).To(ContainSubstring("Invalid policy ID format"))
		})

		It("should return AlreadyExists error for duplicate ID", func() {
			clientID := "duplicate-policy"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}

			// Create first policy
			_, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			// Try to create duplicate
			_, err = policyService.CreatePolicy(ctx, policy, &clientID)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeAlreadyExists))
			Expect(serviceErr.Message).To(ContainSubstring("Policy already exists"))
			Expect(serviceErr.Detail).To(ContainSubstring("duplicate-policy"))
		})

		It("should preserve original Rego when duplicate ID create fails", func() {
			clientID := "duplicate-rego-preserved"
			originalRego := "package original\nallow = true"
			policy1 := v1alpha1.Policy{
				DisplayName: strPtr("First Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr(originalRego),
			}

			_, err := policyService.CreatePolicy(ctx, policy1, &clientID)
			Expect(err).ToNot(HaveOccurred())

			policy2 := v1alpha1.Policy{
				DisplayName: strPtr("Second Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package overwrite\nallow = false"),
			}
			_, err = policyService.CreatePolicy(ctx, policy2, &clientID)
			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeAlreadyExists))

			retrieved, err := policyService.GetPolicy(ctx, clientID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.RegoCode).NotTo(BeNil())
			Expect(*retrieved.RegoCode).To(Equal(originalRego))
		})

		It("should return AlreadyExists when creating two policies with same display_name and policy_type", func() {
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Unique Display Name"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}
			id1 := "policy-dn-1"
			_, err := policyService.CreatePolicy(ctx, policy, &id1)
			Expect(err).ToNot(HaveOccurred())

			id2 := "policy-dn-2"
			_, err = policyService.CreatePolicy(ctx, policy, &id2)
			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeAlreadyExists))
			Expect(serviceErr.Message).To(ContainSubstring("Policy display name and policy type"))
		})

		It("should return AlreadyExists when creating two policies with same priority and policy_type", func() {
			priority := int32(100)
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Policy One"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
				Priority:    &priority,
			}
			id1 := "policy-prio-1"
			_, err := policyService.CreatePolicy(ctx, policy, &id1)
			Expect(err).ToNot(HaveOccurred())

			policy2 := v1alpha1.Policy{
				DisplayName: strPtr("Policy Two"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
				Priority:    &priority,
			}
			id2 := "policy-prio-2"
			_, err = policyService.CreatePolicy(ctx, policy2, &id2)
			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeAlreadyExists))
			Expect(serviceErr.Message).To(ContainSubstring("Policy priority and policy type"))
		})

		It("should use default values for optional fields", func() {
			clientID := "defaults-test"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}

			created, err := policyService.CreatePolicy(ctx, policy, &clientID)

			Expect(err).ToNot(HaveOccurred())
			Expect(*created.Enabled).To(BeTrue())
			Expect(*created.Priority).To(Equal(int32(500)))
		})

		It("should honor explicit values for optional fields", func() {
			clientID := "explicit-values"
			enabled := false
			priority := int32(100)
			description := "Custom description"
			labelSelector := map[string]string{"env": "prod"}

			policy := v1alpha1.Policy{
				DisplayName:   strPtr("Test Policy"),
				PolicyType:    policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:      strPtr("package test"),
				Enabled:       &enabled,
				Priority:      &priority,
				Description:   &description,
				LabelSelector: &labelSelector,
			}

			created, err := policyService.CreatePolicy(ctx, policy, &clientID)

			Expect(err).ToNot(HaveOccurred())
			Expect(*created.Enabled).To(BeFalse())
			Expect(*created.Priority).To(Equal(int32(100)))
			Expect(*created.Description).To(Equal("Custom description"))
			Expect(*created.LabelSelector).To(Equal(map[string]string{"env": "prod"}))
		})

		It("should reject invalid Rego code", func() {
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test\n{invalid rego"),
			}

			_, err := policyService.CreatePolicy(ctx, policy, nil)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
			Expect(serviceErr.Message).To(ContainSubstring("Invalid Rego code"))
		})

		It("should store rego_code in DB and return it on Get", func() {
			clientID := "rego-store-test"
			regoCode := "package test\ndefault allow = false"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Store Rego Test"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr(regoCode),
			}

			_, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			// Verify GET returns Rego from DB
			retrieved, err := policyService.GetPolicy(ctx, clientID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.RegoCode).NotTo(BeNil())
			Expect(*retrieved.RegoCode).To(Equal(regoCode))
		})
	})

	Describe("GetPolicy", func() {
		It("should get existing policy with RegoCode from DB", func() {
			clientID := "get-test"
			regoCode := "package test\ndefault allow = true"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr(regoCode),
			}
			created, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			retrieved, err := policyService.GetPolicy(ctx, "get-test")

			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved).NotTo(BeNil())
			Expect(*retrieved.Id).To(Equal("get-test"))
			Expect(*retrieved.Path).To(Equal("policies/get-test"))
			Expect(retrieved.DisplayName).NotTo(BeNil())
			Expect(*retrieved.DisplayName).To(Equal("Test Policy"))
			Expect(retrieved.RegoCode).NotTo(BeNil())
			Expect(*retrieved.RegoCode).To(Equal(regoCode))
			Expect(retrieved.CreateTime).To(Equal(created.CreateTime))
		})

		It("should return NotFound error for non-existent policy", func() {
			_, err := policyService.GetPolicy(ctx, "non-existent")

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeNotFound))
			Expect(serviceErr.Message).To(ContainSubstring("Policy not found"))
		})
	})

	Describe("ListPolicies", func() {
		BeforeEach(func() {
			// Create test policies
			policies := []struct {
				id         string
				policyType v1alpha1.PolicyPolicyType
				enabled    bool
				priority   int32
			}{
				{"policy-1", v1alpha1.GLOBAL, true, 100},
				{"policy-2", v1alpha1.USER, true, 200},
				{"policy-3", v1alpha1.GLOBAL, false, 300},
				{"policy-4", v1alpha1.USER, false, 400},
			}

			for _, p := range policies {
				enabled := p.enabled
				priority := p.priority
				displayName := "Test " + p.id
				policy := v1alpha1.Policy{
					DisplayName: &displayName,
					PolicyType:  policyTypePtr(p.policyType),
					RegoCode:    strPtr("package test"),
					Enabled:     &enabled,
					Priority:    &priority,
				}
				id := p.id
				_, err := policyService.CreatePolicy(ctx, policy, &id)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		It("should list all policies with default ordering", func() {
			result, err := policyService.ListPolicies(ctx, nil, nil, nil, nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Policies).To(HaveLen(4))
			// Default order is policy_type ASC, priority ASC, id ASC
			// GLOBAL policies first, then USER policies
			Expect(*result.Policies[0].Id).To(Equal("policy-1")) // GLOBAL, priority 100
			Expect(*result.Policies[1].Id).To(Equal("policy-3")) // GLOBAL, priority 300
			Expect(*result.Policies[2].Id).To(Equal("policy-2")) // USER, priority 200
			Expect(*result.Policies[3].Id).To(Equal("policy-4")) // USER, priority 400
		})

		It("should filter by policy_type=GLOBAL", func() {
			filter := "policy_type='GLOBAL'"
			result, err := policyService.ListPolicies(ctx, &filter, nil, nil, nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(result.Policies).To(HaveLen(2))
			for _, p := range result.Policies {
				Expect(p.PolicyType).NotTo(BeNil())
				Expect(*p.PolicyType).To(Equal(v1alpha1.GLOBAL))
			}
		})

		It("should filter by policy_type=USER", func() {
			filter := "policy_type='USER'"
			result, err := policyService.ListPolicies(ctx, &filter, nil, nil, nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(result.Policies).To(HaveLen(2))
			for _, p := range result.Policies {
				Expect(p.PolicyType).NotTo(BeNil())
				Expect(*p.PolicyType).To(Equal(v1alpha1.USER))
			}
		})

		It("should filter by enabled=true", func() {
			filter := "enabled=true"
			result, err := policyService.ListPolicies(ctx, &filter, nil, nil, nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(result.Policies).To(HaveLen(2))
			for _, p := range result.Policies {
				Expect(*p.Enabled).To(BeTrue())
			}
		})

		It("should filter by enabled=false", func() {
			filter := "enabled=false"
			result, err := policyService.ListPolicies(ctx, &filter, nil, nil, nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(result.Policies).To(HaveLen(2))
			for _, p := range result.Policies {
				Expect(*p.Enabled).To(BeFalse())
			}
		})

		It("should filter by combined conditions", func() {
			filter := "policy_type='GLOBAL' AND enabled=true"
			result, err := policyService.ListPolicies(ctx, &filter, nil, nil, nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(result.Policies).To(HaveLen(1))
			Expect(*result.Policies[0].Id).To(Equal("policy-1"))
		})

		It("should order by priority desc", func() {
			orderBy := "priority desc"
			result, err := policyService.ListPolicies(ctx, nil, &orderBy, nil, nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(result.Policies).To(HaveLen(4))
			Expect(*result.Policies[0].Id).To(Equal("policy-4"))
			Expect(*result.Policies[1].Id).To(Equal("policy-3"))
			Expect(*result.Policies[2].Id).To(Equal("policy-2"))
			Expect(*result.Policies[3].Id).To(Equal("policy-1"))
		})

		It("should support pagination", func() {
			pageSize := int32(2)
			result, err := policyService.ListPolicies(ctx, nil, nil, nil, &pageSize)

			Expect(err).ToNot(HaveOccurred())
			Expect(result.Policies).To(HaveLen(2))
			Expect(result.NextPageToken).NotTo(BeNil())

			// Get next page
			result2, err := policyService.ListPolicies(ctx, nil, nil, result.NextPageToken, &pageSize)

			Expect(err).ToNot(HaveOccurred())
			Expect(result2.Policies).To(HaveLen(2))
			Expect(result2.NextPageToken).To(BeNil()) // No more pages
		})

		It("should validate page size minimum", func() {
			pageSize := int32(0)
			_, err := policyService.ListPolicies(ctx, nil, nil, nil, &pageSize)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
			Expect(serviceErr.Message).To(ContainSubstring("Invalid page size"))
		})

		It("should validate page size maximum", func() {
			pageSize := int32(1001)
			_, err := policyService.ListPolicies(ctx, nil, nil, nil, &pageSize)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
			Expect(serviceErr.Message).To(ContainSubstring("Invalid page size"))
		})

		It("should return error for invalid filter", func() {
			filter := "invalid_field='value'"
			_, err := policyService.ListPolicies(ctx, &filter, nil, nil, nil)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
		})

		It("should return error for invalid page_token", func() {
			pageToken := "not-valid-base64!!!"
			_, err := policyService.ListPolicies(ctx, nil, nil, &pageToken, nil)

			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, store.ErrInvalidPageToken)).To(BeTrue())
		})

		It("should return error for invalid order by", func() {
			orderBy := "invalid_field asc"
			_, err := policyService.ListPolicies(ctx, nil, &orderBy, nil, nil)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
		})
	})

	Describe("UpdatePolicy", func() {
		It("should update mutable fields (partial patch)", func() {
			// Create a policy
			clientID := "update-test"
			enabled := true
			priority := int32(100)
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Original Name"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package original"),
				Enabled:     &enabled,
				Priority:    &priority,
			}
			created, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			// PATCH: only update display_name, enabled, priority, description
			newEnabled := false
			newPriority := int32(200)
			newDescription := "Updated description"
			displayName := "Updated Name"
			patch := &v1alpha1.Policy{
				DisplayName: &displayName,
				Enabled:     &newEnabled,
				Priority:    &newPriority,
				Description: &newDescription,
			}

			updated, err := policyService.UpdatePolicy(ctx, "update-test", patch)

			Expect(err).ToNot(HaveOccurred())
			Expect(updated.DisplayName).NotTo(BeNil())
			Expect(*updated.DisplayName).To(Equal("Updated Name"))
			Expect(*updated.Enabled).To(BeFalse())
			Expect(*updated.Priority).To(Equal(int32(200)))
			Expect(*updated.Description).To(Equal("Updated description"))
			Expect(*updated.Id).To(Equal("update-test"))                // ID unchanged
			Expect(updated.CreateTime).To(Equal(created.CreateTime))    // CreateTime unchanged
			Expect(updated.UpdateTime).NotTo(Equal(created.UpdateTime)) // UpdateTime changed
		})

		It("should update Rego code and recompile engine", func() {
			clientID := "update-rego-test"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test\ndefault allow = false"),
			}
			_, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			newRego := "package test\ndefault allow = true"
			patch := &v1alpha1.Policy{
				RegoCode: &newRego,
			}
			_, err = policyService.UpdatePolicy(ctx, clientID, patch)
			Expect(err).ToNot(HaveOccurred())

			// Verify GET returns new Rego
			retrieved, err := policyService.GetPolicy(ctx, clientID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*retrieved.RegoCode).To(Equal(newRego))
		})

		It("should validate RegoCode is non-empty when provided in patch", func() {
			clientID := "update-rego-empty-test"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}
			_, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			emptyRego := ""
			patch := &v1alpha1.Policy{
				RegoCode: &emptyRego,
			}

			_, err = policyService.UpdatePolicy(ctx, "update-rego-empty-test", patch)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
		})

		It("should reject invalid Rego on update", func() {
			clientID := "update-invalid-rego-test"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}
			_, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			invalidRego := "package test\n{invalid rego"
			patch := &v1alpha1.Policy{
				RegoCode: &invalidRego,
			}
			_, err = policyService.UpdatePolicy(ctx, clientID, patch)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))

			// Verify old Rego still in DB
			retrieved, err := policyService.GetPolicy(ctx, clientID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*retrieved.RegoCode).To(Equal("package test"))
		})

		It("should not recompile when RegoCode not in patch", func() {
			clientID := "update-no-rego-test"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}
			_, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			patch := &v1alpha1.Policy{
				DisplayName: strPtr("Updated Name"),
			}
			updated, err := policyService.UpdatePolicy(ctx, clientID, patch)

			Expect(err).ToNot(HaveOccurred())
			Expect(updated).NotTo(BeNil())
			Expect(*updated.DisplayName).To(Equal("Updated Name"))
			// Rego should be preserved
			retrieved, err := policyService.GetPolicy(ctx, clientID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*retrieved.RegoCode).To(Equal("package test"))
		})

		It("should return NotFound error for non-existent policy", func() {
			displayName := "Test"
			patch := &v1alpha1.Policy{
				DisplayName: &displayName,
			}

			_, err := policyService.UpdatePolicy(ctx, "non-existent", patch)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeNotFound))
		})

		It("should return AlreadyExists when updating to another policy's display_name and policy_type", func() {
			regoCode := "package test"
			prioA := int32(200)
			prioB := int32(300)
			idA := "update-dn-a"
			idB := "update-dn-b"
			_, err := policyService.CreatePolicy(ctx, v1alpha1.Policy{
				DisplayName: strPtr("Name A"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    &regoCode,
				Priority:    &prioA,
			}, &idA)
			Expect(err).ToNot(HaveOccurred())
			_, err = policyService.CreatePolicy(ctx, v1alpha1.Policy{
				DisplayName: strPtr("Name B"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    &regoCode,
				Priority:    &prioB,
			}, &idB)
			Expect(err).ToNot(HaveOccurred())

			displayNameA := "Name A"
			patch := &v1alpha1.Policy{
				DisplayName: &displayNameA,
				Priority:    &prioB,
			}
			_, err = policyService.UpdatePolicy(ctx, "update-dn-b", patch)
			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeAlreadyExists))
			Expect(serviceErr.Message).To(ContainSubstring("Policy display name and policy type"))
		})

		It("should return AlreadyExists when updating to another policy's priority and policy_type", func() {
			regoCode := "package test"
			prio200 := int32(200)
			prio300 := int32(300)
			idA := "update-prio-a"
			idB := "update-prio-b"
			_, err := policyService.CreatePolicy(ctx, v1alpha1.Policy{
				DisplayName: strPtr("Policy A"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    &regoCode,
				Priority:    &prio200,
			}, &idA)
			Expect(err).ToNot(HaveOccurred())
			_, err = policyService.CreatePolicy(ctx, v1alpha1.Policy{
				DisplayName: strPtr("Policy B"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    &regoCode,
				Priority:    &prio300,
			}, &idB)
			Expect(err).ToNot(HaveOccurred())

			displayNameB := "Policy B"
			patch := &v1alpha1.Policy{
				DisplayName: &displayNameB,
				Priority:    &prio200,
			}
			_, err = policyService.UpdatePolicy(ctx, "update-prio-b", patch)
			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeAlreadyExists))
			Expect(serviceErr.Message).To(ContainSubstring("Policy priority and policy type"))
		})

		It("should reject update with priority below minimum (0)", func() {
			clientID := "update-prio-min-test"
			priority := int32(500)
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
				Priority:    &priority,
			}
			_, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			invalidPriority := int32(0)
			patch := &v1alpha1.Policy{
				Priority: &invalidPriority,
			}

			_, err = policyService.UpdatePolicy(ctx, "update-prio-min-test", patch)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
			Expect(serviceErr.Message).To(ContainSubstring("priority must be between 1 and 1000"))
		})

		It("should reject update with priority above maximum (1001)", func() {
			clientID := "update-prio-max-test"
			priority := int32(500)
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
				Priority:    &priority,
			}
			_, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			invalidPriority := int32(1001)
			patch := &v1alpha1.Policy{
				Priority: &invalidPriority,
			}

			_, err = policyService.UpdatePolicy(ctx, "update-prio-max-test", patch)

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
			Expect(serviceErr.Message).To(ContainSubstring("priority must be between 1 and 1000"))
		})

		It("should accept update with valid priority (800)", func() {
			clientID := "update-prio-valid-test"
			priority := int32(500)
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test Policy"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
				Priority:    &priority,
			}
			_, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			newPriority := int32(800)
			patch := &v1alpha1.Policy{
				Priority: &newPriority,
			}

			updated, err := policyService.UpdatePolicy(ctx, "update-prio-valid-test", patch)

			Expect(err).ToNot(HaveOccurred())
			Expect(updated).NotTo(BeNil())
			Expect(*updated.Priority).To(Equal(int32(800)))
		})

		// Immutable/readOnly field validation: patch must not change path, id, policy_type, create_time, update_time.
		It("should reject patch when path is different from existing", func() {
			clientID := "immutable-path-test"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Path Test"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}
			_, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			wrongPath := "policies/other-id"
			patch := &v1alpha1.Policy{
				Path:        &wrongPath,
				DisplayName: strPtr("Updated"),
			}
			_, err = policyService.UpdatePolicy(ctx, clientID, patch)
			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
			Expect(serviceErr.Message).To(ContainSubstring("path cannot be updated"))
		})

		It("should accept patch when path is same as existing (with mutable change)", func() {
			clientID := "immutable-path-same-test"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Path Same Test"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}
			created, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())
			Expect(created.Path).NotTo(BeNil())

			patch := &v1alpha1.Policy{
				Path:        created.Path,
				DisplayName: strPtr("Updated Name"),
			}
			updated, err := policyService.UpdatePolicy(ctx, clientID, patch)
			Expect(err).ToNot(HaveOccurred())
			Expect(*updated.DisplayName).To(Equal("Updated Name"))
			Expect(*updated.Path).To(Equal("policies/" + clientID))
		})

		It("should reject patch when id is different from existing", func() {
			clientID := "immutable-id-test"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("ID Test"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}
			_, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			wrongID := "other-id"
			patch := &v1alpha1.Policy{
				Id:          &wrongID,
				DisplayName: strPtr("Updated"),
			}
			_, err = policyService.UpdatePolicy(ctx, clientID, patch)
			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
			Expect(serviceErr.Message).To(ContainSubstring("id cannot be updated"))
		})

		It("should reject patch when policy_type is different from existing", func() {
			clientID := "immutable-type-test"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Type Test"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}
			_, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			patch := &v1alpha1.Policy{
				PolicyType:  policyTypePtr(v1alpha1.USER),
				DisplayName: strPtr("Updated"),
			}
			_, err = policyService.UpdatePolicy(ctx, clientID, patch)
			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
			Expect(serviceErr.Message).To(ContainSubstring("policy_type is immutable"))
		})

		It("should accept patch when policy_type is same as existing (with mutable change)", func() {
			clientID := "immutable-type-same-test"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Type Same Test"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}
			created, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			patch := &v1alpha1.Policy{
				PolicyType:  created.PolicyType,
				DisplayName: strPtr("Updated Name"),
			}
			updated, err := policyService.UpdatePolicy(ctx, clientID, patch)
			Expect(err).ToNot(HaveOccurred())
			Expect(*updated.DisplayName).To(Equal("Updated Name"))
			Expect(*updated.PolicyType).To(Equal(v1alpha1.GLOBAL))
		})

		It("should reject patch when create_time is different from existing", func() {
			clientID := "immutable-ctime-test"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("CreateTime Test"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}
			_, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			otherTime := time.Now().Add(-24 * time.Hour)
			patch := &v1alpha1.Policy{
				CreateTime:  &otherTime,
				DisplayName: strPtr("Updated"),
			}
			_, err = policyService.UpdatePolicy(ctx, clientID, patch)
			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
			Expect(serviceErr.Message).To(ContainSubstring("create_time cannot be updated"))
		})

		It("should reject patch when update_time is different from existing", func() {
			clientID := "immutable-utime-test"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("UpdateTime Test"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}
			created, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			otherTime := time.Now().Add(24 * time.Hour)
			patch := &v1alpha1.Policy{
				UpdateTime:  &otherTime,
				DisplayName: strPtr("Updated"),
			}
			_, err = policyService.UpdatePolicy(ctx, clientID, patch)
			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeInvalidArgument))
			Expect(serviceErr.Message).To(ContainSubstring("update_time cannot be updated"))
			// Ensure existing policy unchanged
			got, _ := policyService.GetPolicy(ctx, clientID)
			Expect(got).NotTo(BeNil())
			Expect(got.UpdateTime).NotTo(BeNil())
			Expect(created.UpdateTime).NotTo(BeNil())
			Expect(got.UpdateTime.Equal(*created.UpdateTime)).To(BeTrue())
		})

		It("should accept patch with only mutable fields (no immutable fields in patch)", func() {
			clientID := "immutable-mutable-only-test"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Mutable Only"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}
			_, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			patch := &v1alpha1.Policy{
				DisplayName: strPtr("Updated Display"),
				Description: strPtr("New description"),
			}
			updated, err := policyService.UpdatePolicy(ctx, clientID, patch)
			Expect(err).ToNot(HaveOccurred())
			Expect(*updated.DisplayName).To(Equal("Updated Display"))
			Expect(updated.Description).NotTo(BeNil())
			Expect(*updated.Description).To(Equal("New description"))
		})

		It("should accept patch with nil immutable fields (field not sent)", func() {
			clientID := "immutable-nil-fields-test"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Nil Fields Test"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}
			created, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			// Patch omits path, id, policy_type, create_time, update_time (all nil)
			patch := &v1alpha1.Policy{
				DisplayName: strPtr("New Name"),
			}
			updated, err := policyService.UpdatePolicy(ctx, clientID, patch)
			Expect(err).ToNot(HaveOccurred())
			Expect(*updated.DisplayName).To(Equal("New Name"))
			Expect(*updated.Id).To(Equal(*created.Id))
			Expect(*updated.Path).To(Equal(*created.Path))
			Expect(*updated.PolicyType).To(Equal(*created.PolicyType))
			Expect(updated.CreateTime).NotTo(BeNil())
			Expect(updated.CreateTime.Equal(*created.CreateTime)).To(BeTrue())
		})
	})

	Describe("DeletePolicy", func() {
		It("should delete existing policy", func() {
			clientID := "delete-test"
			policy := v1alpha1.Policy{
				DisplayName: strPtr("Test"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test"),
			}
			_, err := policyService.CreatePolicy(ctx, policy, &clientID)
			Expect(err).ToNot(HaveOccurred())

			err = policyService.DeletePolicy(ctx, "delete-test")

			Expect(err).ToNot(HaveOccurred())

			// Verify it's deleted
			_, err = policyService.GetPolicy(ctx, "delete-test")
			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeNotFound))
		})

		It("should return NotFound error for non-existent policy", func() {
			err := policyService.DeletePolicy(ctx, "non-existent")

			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeNotFound))
		})
	})

	Describe("Rollback on compile failure", func() {
		It("rolls back DB create when recompile fails with canceled request context", func() {
			clientID := "rollback-create-test"
			mockEngine := &rollbackMockEngine{}
			svc := service.NewPolicyService(dataStore, mockEngine)

			reqCtx, cancelReq := context.WithCancel(ctx)
			mockEngine.compileFunc = func(context.Context, []opa.PolicyModule) error {
				cancelReq()
				return context.Canceled
			}

			policy := v1alpha1.Policy{
				DisplayName: strPtr("Rollback Create Test"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr("package test\ndefault allow = true"),
			}

			_, err := svc.CreatePolicy(reqCtx, policy, &clientID)
			Expect(err).To(HaveOccurred())

			_, err = svc.GetPolicy(ctx, clientID)
			Expect(err).To(HaveOccurred())
			serviceErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(serviceErr.Type).To(Equal(service.ErrorTypeNotFound))
		})

		It("rolls back DB update when recompile fails with canceled request context", func() {
			clientID := "rollback-update-test"
			originalRego := "package test\ndefault allow = true"
			updatedRego := "package test\ndefault allow = false"

			realEngine := opa.NewEngine()
			svc := service.NewPolicyService(dataStore, realEngine)

			_, err := svc.CreatePolicy(ctx, v1alpha1.Policy{
				DisplayName: strPtr("Rollback Update Test"),
				PolicyType:  policyTypePtr(v1alpha1.GLOBAL),
				RegoCode:    strPtr(originalRego),
			}, &clientID)
			Expect(err).ToNot(HaveOccurred())

			mockEngine := &rollbackMockEngine{}
			svc = service.NewPolicyService(dataStore, mockEngine)

			reqCtx, cancelReq := context.WithCancel(ctx)
			mockEngine.compileFunc = func(context.Context, []opa.PolicyModule) error {
				cancelReq()
				return context.Canceled
			}

			patch := &v1alpha1.Policy{RegoCode: strPtr(updatedRego)}
			_, err = svc.UpdatePolicy(reqCtx, clientID, patch)
			Expect(err).To(HaveOccurred())

			retrieved, err := svc.GetPolicy(ctx, clientID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.RegoCode).NotTo(BeNil())
			Expect(*retrieved.RegoCode).To(Equal(originalRego))
		})
	})
})
