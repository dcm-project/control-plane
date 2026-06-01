package service

import (
	"context"
	"errors"

	"github.com/dcm-project/control-plane/internal/policy/opa"
	"github.com/dcm-project/control-plane/internal/policy/store"
	"github.com/dcm-project/control-plane/internal/policy/store/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Test suite is registered in other test files - don't register again

// Mock implementations
type mockPolicyStore struct {
	policies []model.Policy
	err      error
}

func (m *mockPolicyStore) Create(_ context.Context, _ model.Policy) (*model.Policy, error) {
	return nil, errors.New("not implemented")
}

func (m *mockPolicyStore) Get(_ context.Context, _ string) (*model.Policy, error) {
	return nil, errors.New("not implemented")
}

func (m *mockPolicyStore) List(_ context.Context, _ *store.PolicyListOptions) (*store.PolicyListResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &store.PolicyListResult{
		Policies: m.policies,
	}, nil
}

func (m *mockPolicyStore) ListAll(_ context.Context) (model.PolicyList, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.policies, nil
}

func (m *mockPolicyStore) Update(_ context.Context, _ model.Policy) (*model.Policy, error) {
	return nil, errors.New("not implemented")
}

func (m *mockPolicyStore) Delete(_ context.Context, _ string) error {
	return errors.New("not implemented")
}

type mockEngine struct {
	evaluations map[string]*opa.EvaluationResult
	err         error
}

func (m *mockEngine) Compile(_ context.Context, _ []opa.PolicyModule) error {
	return nil
}

func (m *mockEngine) ValidateRego(_ context.Context, _ string) error {
	return nil
}

func (m *mockEngine) EvaluatePolicy(_ context.Context, policyID string, _ map[string]any) (*opa.EvaluationResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	if result, ok := m.evaluations[policyID]; ok {
		return result, nil
	}
	// Return undefined result by default
	return &opa.EvaluationResult{Defined: false}, nil
}

var _ = Describe("EvaluationService", func() {
	var (
		ctx         context.Context
		mockStore   *mockPolicyStore
		mockOPA     *mockEngine
		service     EvaluationService
		baseRequest *EvaluationRequest
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockStore = &mockPolicyStore{
			policies: []model.Policy{},
		}
		mockOPA = &mockEngine{
			evaluations: make(map[string]*opa.EvaluationResult),
		}
		service = NewEvaluationService(mockStore, mockOPA)

		baseRequest = &EvaluationRequest{
			ServiceInstance: map[string]any{},
			RequestLabels:   map[string]string{},
		}
	})

	Describe("EvaluateRequest", func() {
		Context("when no policies exist", func() {
			It("returns approved with unchanged spec", func() {
				response, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response.Status).To(Equal(EvaluationStatusApproved))
				Expect(response.EvaluatedServiceInstance).To(Equal(map[string]any{}))
				Expect(response.SelectedProvider).To(Equal(""))
			})
		})

		Context("when policies don't match label selectors", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:            "policy-1",
						Enabled:       true,
						PolicyType:    "GLOBAL",
						Priority:      100,
						LabelSelector: map[string]string{"env": "prod"},
					},
				}
			})

			It("returns approved with unchanged spec", func() {
				baseRequest.RequestLabels = map[string]string{"env": "dev"}

				response, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response.Status).To(Equal(EvaluationStatusApproved))
			})
		})

		Context("when policy modifies the spec via patch", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
				}

				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"region": "us-east-1",
						},
						"selected_provider": "aws",
					},
				}
			})

			It("returns modified with updated spec", func() {
				response, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response.Status).To(Equal(EvaluationStatusModified))
				Expect(response.EvaluatedServiceInstance).To(Equal(map[string]any{
					"region": "us-east-1",
				}))
				Expect(response.SelectedProvider).To(Equal("aws"))
			})
		})

		Context("when patch merges with existing spec", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
				}

				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"region": "us-east-1",
						},
					},
				}
			})

			It("preserves existing spec fields not in patch", func() {
				baseRequest.ServiceInstance = map[string]any{
					"instance_type": "t3.medium",
				}

				response, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response.Status).To(Equal(EvaluationStatusModified))
				Expect(response.EvaluatedServiceInstance).To(Equal(map[string]any{
					"instance_type": "t3.medium",
					"region":        "us-east-1",
				}))
			})
		})

		Context("when policy rejects the request", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
				}

				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected":         true,
						"rejection_reason": "Security policy violation",
					},
				}
			})

			It("returns policy rejected error", func() {
				_, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).To(HaveOccurred())
				serviceErr, ok := err.(*ServiceError)
				Expect(ok).To(BeTrue())
				Expect(serviceErr.Type).To(Equal(ErrorTypeRejected))
				Expect(serviceErr.Message).To(ContainSubstring("policy-1"))
				Expect(serviceErr.Detail).To(Equal("Security policy violation"))
			})
		})

		Context("when lower-priority policy violates constraint", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
					{
						ID:         "policy-2",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   200,
					},
				}

				// First policy sets region with a const constraint
				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"region": "us-east-1",
						},
						"constraints": map[string]any{
							"region": map[string]any{
								"const": "us-east-1",
							},
						},
					},
				}

				// Second policy tries to change region, violating the constraint
				mockOPA.evaluations["policy-2"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"region": "us-west-2",
						},
					},
				}
			})

			It("returns policy conflict error", func() {
				_, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).To(HaveOccurred())
				serviceErr, ok := err.(*ServiceError)
				Expect(ok).To(BeTrue())
				Expect(serviceErr.Type).To(Equal(ErrorTypePolicyConflict))
				Expect(serviceErr.Message).To(ContainSubstring("policy-2"))
			})
		})

		Context("when lower-priority policy tries to loosen constraint", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
					{
						ID:         "policy-2",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   200,
					},
				}

				// First policy sets minimum constraint
				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"constraints": map[string]any{
							"cpu_count": map[string]any{
								"minimum": float64(4),
								"maximum": float64(8),
							},
						},
					},
				}

				// Second policy tries to lower the minimum — loosening
				mockOPA.evaluations["policy-2"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"constraints": map[string]any{
							"cpu_count": map[string]any{
								"minimum": float64(1),
							},
						},
					},
				}
			})

			It("returns constraint conflict error", func() {
				_, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).To(HaveOccurred())
				serviceErr, ok := err.(*ServiceError)
				Expect(ok).To(BeTrue())
				Expect(serviceErr.Type).To(Equal(ErrorTypePolicyConflict))
				Expect(serviceErr.Message).To(ContainSubstring("policy-2"))
				Expect(serviceErr.Detail).To(ContainSubstring("loosen"))
			})
		})

		Context("when policies evaluate sequentially without conflicts", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
					{
						ID:         "policy-2",
						Enabled:    true,
						PolicyType: "USER",
						Priority:   100,
					},
				}

				// First policy adds region
				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"region": "us-east-1",
						},
					},
				}

				// Second policy adds instance_type (no conflict — no constraint on region)
				mockOPA.evaluations["policy-2"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"instance_type": "t3.medium",
						},
					},
				}
			})

			It("applies both policies successfully", func() {
				response, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response.Status).To(Equal(EvaluationStatusModified))
				Expect(response.EvaluatedServiceInstance).To(Equal(map[string]any{
					"region":        "us-east-1",
					"instance_type": "t3.medium",
				}))
			})
		})

		Context("when policy sets value with range constraint allowing further changes", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
					{
						ID:         "policy-2",
						Enabled:    true,
						PolicyType: "USER",
						Priority:   100,
					},
				}

				// First policy sets cpu_count=2 with range constraint 1-4
				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"cpu_count": float64(2),
						},
						"constraints": map[string]any{
							"cpu_count": map[string]any{
								"minimum": float64(1),
								"maximum": float64(4),
							},
						},
					},
				}

				// Second policy changes cpu_count to 4 — within constraint range
				mockOPA.evaluations["policy-2"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"cpu_count": float64(4),
						},
					},
				}
			})

			It("allows the change within the constraint range", func() {
				response, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response.Status).To(Equal(EvaluationStatusModified))
				Expect(response.EvaluatedServiceInstance["cpu_count"]).To(Equal(float64(4)))
			})
		})

		Context("when policy evaluation fails", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
				}

				mockOPA.err = errors.New("OPA unavailable")
			})

			It("returns internal error", func() {
				_, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).To(HaveOccurred())
				serviceErr, ok := err.(*ServiceError)
				Expect(ok).To(BeTrue())
				Expect(serviceErr.Type).To(Equal(ErrorTypeInternal))
			})
		})

		Context("when label selector matches", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,

						LabelSelector: map[string]string{"env": "prod", "team": "backend"},
					},
				}

				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"region": "us-east-1",
						},
					},
				}
			})

			It("applies policy when all labels match", func() {
				baseRequest.RequestLabels = map[string]string{
					"env":  "prod",
					"team": "backend",
					"app":  "web", // Extra label is OK
				}

				response, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response.Status).To(Equal(EvaluationStatusModified))
			})

			It("skips policy when labels don't match", func() {
				baseRequest.RequestLabels = map[string]string{
					"env": "prod",
					// Missing "team" label
				}

				response, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response.Status).To(Equal(EvaluationStatusApproved))
			})
		})

		Context("when OPA input includes accumulated constraints", func() {
			var capturedInput map[string]any

			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
					{
						ID:         "policy-2",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   200,
					},
				}

				// First policy sets constraint
				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"constraints": map[string]any{
							"region": map[string]any{
								"enum": []any{"us-east-1", "us-west-2"},
							},
						},
					},
				}

				// Capture input for second policy
				mockOPA.evaluations["policy-2"] = &opa.EvaluationResult{
					Defined: false,
				}
			})

			It("passes constraints in OPA input for subsequent policies", func() {
				// Override the mock to capture the input
				originalEval := mockOPA.evaluations
				mockOPA.evaluations = nil

				evalCount := 0
				customOPA := &mockEngineWithCapture{
					evaluations: originalEval,
					captureFunc: func(input map[string]any) {
						evalCount++
						if evalCount == 2 {
							capturedInput = input
						}
					},
				}

				service = NewEvaluationService(mockStore, customOPA)
				_, _ = service.EvaluateRequest(ctx, baseRequest)

				Expect(capturedInput).To(HaveKey("constraints"))
			})
		})

		Context("when service provider constraints are enforced", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
					{
						ID:         "policy-2",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   200,
					},
				}

				// First policy sets SP constraint allow list
				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"service_provider_constraints": map[string]any{
							"allow_list": []any{"aws", "gcp"},
						},
					},
				}

				// Second policy selects a provider not in allow list
				mockOPA.evaluations["policy-2"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected":          false,
						"selected_provider": "azure",
					},
				}
			})

			It("returns SP constraint error", func() {
				_, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).To(HaveOccurred())
				serviceErr, ok := err.(*ServiceError)
				Expect(ok).To(BeTrue())
				Expect(serviceErr.Type).To(Equal(ErrorTypePolicyConflict))
				Expect(serviceErr.Message).To(ContainSubstring("policy-2"))
				Expect(serviceErr.Detail).To(ContainSubstring("not in the allowed list"))
			})
		})
	})
})

// mockEngineWithCapture wraps mockEngine and captures inputs
type mockEngineWithCapture struct {
	evaluations map[string]*opa.EvaluationResult
	captureFunc func(input map[string]any)
}

func (m *mockEngineWithCapture) Compile(_ context.Context, _ []opa.PolicyModule) error {
	return nil
}

func (m *mockEngineWithCapture) ValidateRego(_ context.Context, _ string) error {
	return nil
}

func (m *mockEngineWithCapture) EvaluatePolicy(_ context.Context, policyID string, input map[string]any) (*opa.EvaluationResult, error) {
	if m.captureFunc != nil {
		m.captureFunc(input)
	}
	if result, ok := m.evaluations[policyID]; ok {
		return result, nil
	}
	return &opa.EvaluationResult{Defined: false}, nil
}
