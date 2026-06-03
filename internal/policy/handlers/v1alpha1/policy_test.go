package v1alpha1

import (
	"context"

	"github.com/dcm-project/control-plane/api/policy/v1alpha1"
	"github.com/dcm-project/control-plane/internal/policy/api/server"
	"github.com/dcm-project/control-plane/internal/policy/service"
	"github.com/dcm-project/control-plane/internal/policy/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// MockPolicyService is a mock implementation of PolicyService for testing
type MockPolicyService struct {
	CreatePolicyFn func(ctx context.Context, policy v1alpha1.Policy, clientID *string) (*v1alpha1.Policy, error)
	GetPolicyFn    func(ctx context.Context, id string) (*v1alpha1.Policy, error)
	ListPoliciesFn func(ctx context.Context, filter *string, orderBy *string, pageToken *string, pageSize *int32) (*v1alpha1.PolicyList, error)
	UpdatePolicyFn func(ctx context.Context, id string, patch *v1alpha1.Policy) (*v1alpha1.Policy, error)
	DeletePolicyFn func(ctx context.Context, id string) error
}

func (m *MockPolicyService) CompileAll(_ context.Context) error {
	return nil
}

func (m *MockPolicyService) CreatePolicy(ctx context.Context, policy v1alpha1.Policy, clientID *string) (*v1alpha1.Policy, error) {
	if m.CreatePolicyFn != nil {
		return m.CreatePolicyFn(ctx, policy, clientID)
	}
	return nil, nil
}

func (m *MockPolicyService) GetPolicy(ctx context.Context, id string) (*v1alpha1.Policy, error) {
	if m.GetPolicyFn != nil {
		return m.GetPolicyFn(ctx, id)
	}
	return nil, nil
}

func (m *MockPolicyService) ListPolicies(ctx context.Context, filter *string, orderBy *string, pageToken *string, pageSize *int32) (*v1alpha1.PolicyList, error) {
	if m.ListPoliciesFn != nil {
		return m.ListPoliciesFn(ctx, filter, orderBy, pageToken, pageSize)
	}
	return nil, nil
}

func (m *MockPolicyService) UpdatePolicy(ctx context.Context, id string, patch *v1alpha1.Policy) (*v1alpha1.Policy, error) {
	if m.UpdatePolicyFn != nil {
		return m.UpdatePolicyFn(ctx, id, patch)
	}
	return nil, nil
}

func (m *MockPolicyService) DeletePolicy(ctx context.Context, id string) error {
	if m.DeletePolicyFn != nil {
		return m.DeletePolicyFn(ctx, id)
	}
	return nil
}

var _ = Describe("PolicyHandler", func() {
	var handler *PolicyHandler
	var mockService *MockPolicyService

	BeforeEach(func() {
		mockService = &MockPolicyService{}
		handler = NewPolicyHandler(mockService)
	})

	Describe("GetHealth", func() {
		It("should return a successful health response with correct status and path", func() {
			ctx := context.Background()
			response, err := handler.GetHealth(ctx, server.GetHealthRequestObject{})

			Expect(err).NotTo(HaveOccurred())
			Expect(response).NotTo(BeNil())

			healthResponse, ok := response.(server.GetHealth200JSONResponse)
			Expect(ok).To(BeTrue(), "response should be GetHealth200JSONResponse")

			Expect(healthResponse.Status).NotTo(BeNil())
			Expect(healthResponse.Status).To(Equal("ok"))

			Expect(healthResponse.Path).NotTo(BeNil())
			Expect(*healthResponse.Path).To(Equal("health"))
		})
	})

	Describe("CreatePolicy", func() {
		It("should return 201 on successful creation", func() {
			ctx := context.Background()
			policyID := "test-policy"
			path := "policies/test-policy"
			enabled := true
			priority := int32(500)

			regoCode := ""
			mockService.CreatePolicyFn = func(_ context.Context, policy v1alpha1.Policy, _ *string) (*v1alpha1.Policy, error) {
				return &v1alpha1.Policy{
					Id:          &policyID,
					Path:        &path,
					DisplayName: policy.DisplayName,
					PolicyType:  policy.PolicyType,
					Enabled:     &enabled,
					Priority:    &priority,
					RegoCode:    &regoCode,
				}, nil
			}

			displayName := "Test Policy"
			regoCodeReq := "package test"
			pt := v1alpha1.GLOBAL
			body := v1alpha1.Policy{
				DisplayName: &displayName,
				PolicyType:  &pt,
				RegoCode:    &regoCodeReq,
			}

			response, err := handler.CreatePolicy(ctx, server.CreatePolicyRequestObject{
				Body: &body,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(response).NotTo(BeNil())

			createResponse, ok := response.(server.CreatePolicy201JSONResponse)
			Expect(ok).To(BeTrue(), "response should be CreatePolicy201JSONResponse")
			Expect(*createResponse.Body.Id).To(Equal("test-policy"))
		})

		It("should return 400 when body is nil", func() {
			ctx := context.Background()

			response, err := handler.CreatePolicy(ctx, server.CreatePolicyRequestObject{
				Body: nil,
			})

			Expect(err).NotTo(HaveOccurred())
			_, ok := response.(server.CreatePolicy400JSONResponse)
			Expect(ok).To(BeTrue(), "response should be CreatePolicy400JSONResponse")
		})

		It("should return 409 when policy already exists", func() {
			ctx := context.Background()

			mockService.CreatePolicyFn = func(_ context.Context, _ v1alpha1.Policy, _ *string) (*v1alpha1.Policy, error) {
				return nil, service.NewAlreadyExistsError("Policy already exists", "Duplicate ID")
			}

			displayName := "Test Policy"
			regoCodeReq := "package test"
			pt := v1alpha1.GLOBAL
			body := v1alpha1.Policy{
				DisplayName: &displayName,
				PolicyType:  &pt,
				RegoCode:    &regoCodeReq,
			}

			response, err := handler.CreatePolicy(ctx, server.CreatePolicyRequestObject{
				Body: &body,
			})

			Expect(err).NotTo(HaveOccurred())
			_, ok := response.(server.CreatePolicy409JSONResponse)
			Expect(ok).To(BeTrue(), "response should be CreatePolicy409JSONResponse")
		})
	})

	Describe("GetPolicy", func() {
		It("should return 200 with policy on success", func() {
			ctx := context.Background()
			policyID := "test-policy"
			path := "policies/test-policy"

			displayName := "Test Policy"
			regoCodeEmpty := ""
			pt := v1alpha1.GLOBAL
			mockService.GetPolicyFn = func(_ context.Context, _ string) (*v1alpha1.Policy, error) {
				return &v1alpha1.Policy{
					Id:          &policyID,
					Path:        &path,
					DisplayName: &displayName,
					PolicyType:  &pt,
					RegoCode:    &regoCodeEmpty,
				}, nil
			}

			response, err := handler.GetPolicy(ctx, server.GetPolicyRequestObject{
				PolicyId: "test-policy",
			})

			Expect(err).NotTo(HaveOccurred())
			policy, ok := response.(server.GetPolicy200JSONResponse)
			Expect(ok).To(BeTrue(), "response should be GetPolicy200JSONResponse")
			Expect(*policy.Id).To(Equal("test-policy"))
		})

		It("should return 404 when policy not found", func() {
			ctx := context.Background()

			mockService.GetPolicyFn = func(_ context.Context, _ string) (*v1alpha1.Policy, error) {
				return nil, service.NewNotFoundError("Policy not found", "Not found")
			}

			response, err := handler.GetPolicy(ctx, server.GetPolicyRequestObject{
				PolicyId: "non-existent",
			})

			Expect(err).NotTo(HaveOccurred())
			_, ok := response.(server.GetPolicy404JSONResponse)
			Expect(ok).To(BeTrue(), "response should be GetPolicy404JSONResponse")
		})
	})

	Describe("ListPolicies", func() {
		It("should return 200 with list of policies", func() {
			ctx := context.Background()
			policyID1 := "policy-1"
			policyID2 := "policy-2"
			path1 := "policies/policy-1"
			path2 := "policies/policy-2"

			displayName1 := "Policy 1"
			displayName2 := "Policy 2"
			regoCodeEmpty := ""
			pt1 := v1alpha1.GLOBAL
			pt2 := v1alpha1.USER
			mockService.ListPoliciesFn = func(_ context.Context, _ *string, _ *string, _ *string, _ *int32) (*v1alpha1.PolicyList, error) {
				return &v1alpha1.PolicyList{
					Policies: []v1alpha1.Policy{
						{
							Id:          &policyID1,
							Path:        &path1,
							DisplayName: &displayName1,
							PolicyType:  &pt1,
							RegoCode:    &regoCodeEmpty,
						},
						{
							Id:          &policyID2,
							Path:        &path2,
							DisplayName: &displayName2,
							PolicyType:  &pt2,
							RegoCode:    &regoCodeEmpty,
						},
					},
					NextPageToken: nil,
				}, nil
			}

			response, err := handler.ListPolicies(ctx, server.ListPoliciesRequestObject{
				Params: v1alpha1.ListPoliciesParams{},
			})

			Expect(err).NotTo(HaveOccurred())
			listResponse, ok := response.(server.ListPolicies200JSONResponse)
			Expect(ok).To(BeTrue(), "response should be ListPolicies200JSONResponse")
			Expect(listResponse.Policies).To(HaveLen(2))
			Expect(*listResponse.Policies[0].Id).To(Equal("policy-1"))
			Expect(*listResponse.Policies[1].Id).To(Equal("policy-2"))
		})

		It("should pass filter parameter to service", func() {
			ctx := context.Background()
			filter := "policy_type='GLOBAL'"
			var receivedFilter *string

			mockService.ListPoliciesFn = func(_ context.Context, filter *string, _ *string, _ *string, _ *int32) (*v1alpha1.PolicyList, error) {
				receivedFilter = filter
				return &v1alpha1.PolicyList{
					Policies: []v1alpha1.Policy{},
				}, nil
			}

			_, err := handler.ListPolicies(ctx, server.ListPoliciesRequestObject{
				Params: v1alpha1.ListPoliciesParams{
					Filter: &filter,
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(receivedFilter).NotTo(BeNil())
			Expect(*receivedFilter).To(Equal("policy_type='GLOBAL'"))
		})

		It("should pass pagination parameters to service", func() {
			ctx := context.Background()
			pageToken := "token123"
			pageSize := int32(10)
			var receivedPageToken *string
			var receivedPageSize *int32

			mockService.ListPoliciesFn = func(_ context.Context, _ *string, _ *string, pageToken *string, pageSize *int32) (*v1alpha1.PolicyList, error) {
				receivedPageToken = pageToken
				receivedPageSize = pageSize
				return &v1alpha1.PolicyList{
					Policies: []v1alpha1.Policy{},
				}, nil
			}

			_, err := handler.ListPolicies(ctx, server.ListPoliciesRequestObject{
				Params: v1alpha1.ListPoliciesParams{
					PageToken:   &pageToken,
					MaxPageSize: &pageSize,
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(receivedPageToken).NotTo(BeNil())
			Expect(*receivedPageToken).To(Equal("token123"))
			Expect(receivedPageSize).NotTo(BeNil())
			Expect(*receivedPageSize).To(Equal(int32(10)))
		})

		It("should return 400 for invalid filter", func() {
			ctx := context.Background()

			mockService.ListPoliciesFn = func(_ context.Context, _ *string, _ *string, _ *string, _ *int32) (*v1alpha1.PolicyList, error) {
				return nil, service.NewInvalidArgumentError("Invalid filter", "Bad filter expression")
			}

			filter := "invalid_field='value'"
			response, err := handler.ListPolicies(ctx, server.ListPoliciesRequestObject{
				Params: v1alpha1.ListPoliciesParams{
					Filter: &filter,
				},
			})

			Expect(err).NotTo(HaveOccurred())
			_, ok := response.(server.ListPolicies400JSONResponse)
			Expect(ok).To(BeTrue(), "response should be ListPolicies400JSONResponse")
		})

		It("should return 400 for invalid page_token", func() {
			ctx := context.Background()

			mockService.ListPoliciesFn = func(_ context.Context, _ *string, _ *string, _ *string, _ *int32) (*v1alpha1.PolicyList, error) {
				return nil, store.ErrInvalidPageToken
			}

			response, err := handler.ListPolicies(ctx, server.ListPoliciesRequestObject{})

			Expect(err).NotTo(HaveOccurred())
			badRequest, ok := response.(server.ListPolicies400JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(badRequest.Status).To(Equal(int32(400)))
		})
	})

	Describe("UpdatePolicy", func() {
		It("should return 200 on successful update", func() {
			ctx := context.Background()
			policyID := "test-policy"
			path := "policies/test-policy"
			enabled := false
			priority := int32(200)
			regoCodeEmpty := ""
			pt := v1alpha1.GLOBAL
			mockService.UpdatePolicyFn = func(_ context.Context, _ string, patch *v1alpha1.Policy) (*v1alpha1.Policy, error) {
				displayName := "Updated Policy"
				if patch != nil && patch.DisplayName != nil {
					displayName = *patch.DisplayName
				}
				return &v1alpha1.Policy{
					Id:          &policyID,
					Path:        &path,
					DisplayName: &displayName,
					PolicyType:  &pt,
					Enabled:     &enabled,
					Priority:    &priority,
					RegoCode:    &regoCodeEmpty,
				}, nil
			}

			displayName := "Updated Policy"
			regoCode := "package updated"
			body := v1alpha1.Policy{
				DisplayName: &displayName,
				RegoCode:    &regoCode,
			}

			response, err := handler.UpdatePolicy(ctx, server.UpdatePolicyRequestObject{
				PolicyId: "test-policy",
				Body:     &body,
			})

			Expect(err).NotTo(HaveOccurred())
			updateResponse, ok := response.(server.UpdatePolicy200JSONResponse)
			Expect(ok).To(BeTrue(), "response should be UpdatePolicy200JSONResponse")
			Expect(*updateResponse.Id).To(Equal("test-policy"))
			Expect(updateResponse.DisplayName).NotTo(BeNil())
			Expect(*updateResponse.DisplayName).To(Equal("Updated Policy"))
		})

		It("should return 400 when body is nil", func() {
			ctx := context.Background()

			response, err := handler.UpdatePolicy(ctx, server.UpdatePolicyRequestObject{
				PolicyId: "test-policy",
				Body:     nil,
			})

			Expect(err).NotTo(HaveOccurred())
			_, ok := response.(server.UpdatePolicy400JSONResponse)
			Expect(ok).To(BeTrue(), "response should be UpdatePolicy400JSONResponse")
		})

		It("should return 404 when policy not found", func() {
			ctx := context.Background()

			mockService.UpdatePolicyFn = func(_ context.Context, _ string, _ *v1alpha1.Policy) (*v1alpha1.Policy, error) {
				return nil, service.NewNotFoundError("Policy not found", "Not found")
			}

			displayName := "Updated Policy"
			regoCode := "package test"
			body := v1alpha1.Policy{
				DisplayName: &displayName,
				RegoCode:    &regoCode,
			}

			response, err := handler.UpdatePolicy(ctx, server.UpdatePolicyRequestObject{
				PolicyId: "non-existent",
				Body:     &body,
			})

			Expect(err).NotTo(HaveOccurred())
			_, ok := response.(server.UpdatePolicy404JSONResponse)
			Expect(ok).To(BeTrue(), "response should be UpdatePolicy404JSONResponse")
		})
	})

	Describe("DeletePolicy", func() {
		It("should return 204 on successful deletion", func() {
			ctx := context.Background()

			mockService.DeletePolicyFn = func(_ context.Context, _ string) error {
				return nil
			}

			response, err := handler.DeletePolicy(ctx, server.DeletePolicyRequestObject{
				PolicyId: "test-policy",
			})

			Expect(err).NotTo(HaveOccurred())
			_, ok := response.(server.DeletePolicy204Response)
			Expect(ok).To(BeTrue(), "response should be DeletePolicy204Response")
		})

		It("should return 404 when policy not found", func() {
			ctx := context.Background()

			mockService.DeletePolicyFn = func(_ context.Context, _ string) error {
				return service.NewNotFoundError("Policy not found", "Not found")
			}

			response, err := handler.DeletePolicy(ctx, server.DeletePolicyRequestObject{
				PolicyId: "non-existent",
			})

			Expect(err).NotTo(HaveOccurred())
			_, ok := response.(server.DeletePolicy404JSONResponse)
			Expect(ok).To(BeTrue(), "response should be DeletePolicy404JSONResponse")
		})
	})
})
