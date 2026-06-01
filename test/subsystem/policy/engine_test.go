//go:build subsystem

package subsystem_test

import (
	"context"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/control-plane/api/policy/v1alpha1"
	engineapi "github.com/dcm-project/control-plane/api/policy/v1alpha1/engine"
	"github.com/dcm-project/control-plane/pkg/policy/client"
	"github.com/dcm-project/control-plane/pkg/policy/engineclient"
)

var _ = Describe("Engine API - Policy Evaluation", func() {
	var (
		engineClient *engineclient.ClientWithResponses
		policyClient *client.ClientWithResponses
		ctx          context.Context
	)

	BeforeEach(func() {
		engineURL := getEnvOrDefault("ENGINE_API_URL", "http://localhost:8081/api/v1alpha1")
		policyURL := getEnvOrDefault("API_URL", "http://localhost:8080/api/v1alpha1")

		var err error
		engineClient, err = engineclient.NewClientWithResponses(engineURL)
		Expect(err).NotTo(HaveOccurred())

		policyClient, err = client.NewClientWithResponses(policyURL)
		Expect(err).NotTo(HaveOccurred())

		ctx = context.Background()
	})

	Describe("POST /policies:evaluateRequest", func() {
		Context("when no policies exist", func() {
			It("should return APPROVED with unchanged spec", func() {
				request := engineapi.EvaluateRequest{
					ServiceInstance: engineapi.ServiceInstance{
						Spec: map[string]any{
							"service_type": "test-service",
							"region":       "us-east-1",
						},
					},
				}

				resp, err := engineClient.EvaluateRequestWithResponse(ctx, request)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusOK))
				Expect(resp.JSON200).NotTo(BeNil())
				Expect(resp.JSON200.Status).To(Equal(engineapi.APPROVED))
				Expect(resp.JSON200.EvaluatedServiceInstance.Spec).To(Equal(request.ServiceInstance.Spec))
				Expect(resp.JSON200.SelectedProvider).To(Equal(""))
			})
		})

		Context("when policy modifies the spec via patch", func() {
			var policyID string

			BeforeEach(func() {
				// Create a policy that patches the spec
				regoCode := `package policies.test_modify

main := {
	"rejected": false,
	"patch": {
		"region": "us-west-2",
		"instance_type": "t3.medium"
	},
	"selected_provider": "aws"
}`
				policyID = "test-modify-policy"
				displayName := "Test Modify Policy"
				policyType := v1alpha1.GLOBAL
				enabled := true
				priority := int32(100)

				createResp, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policyID,
				}, v1alpha1.Policy{
					DisplayName: &displayName,
					PolicyType:  &policyType,
					RegoCode:    &regoCode,
					Enabled:     &enabled,
					Priority:    &priority,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			})

			AfterEach(func() {
				policyClient.DeletePolicyWithResponse(ctx, policyID)
			})

			It("should return MODIFIED with updated spec preserving existing fields", func() {
				request := engineapi.EvaluateRequest{
					ServiceInstance: engineapi.ServiceInstance{
						Spec: map[string]any{
							"service_type":   "test-service",
							"existing_field": "keep-me",
						},
					},
				}

				resp, err := engineClient.EvaluateRequestWithResponse(ctx, request)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusOK))
				Expect(resp.JSON200).NotTo(BeNil())
				Expect(resp.JSON200.Status).To(Equal(engineapi.MODIFIED))
				Expect(resp.JSON200.EvaluatedServiceInstance.Spec["region"]).To(Equal("us-west-2"))
				Expect(resp.JSON200.EvaluatedServiceInstance.Spec["instance_type"]).To(Equal("t3.medium"))
				Expect(resp.JSON200.EvaluatedServiceInstance.Spec["existing_field"]).To(Equal("keep-me"))
				Expect(resp.JSON200.SelectedProvider).To(Equal("aws"))
			})
		})

		Context("when policy rejects the request", func() {
			var policyID string

			BeforeEach(func() {
				// Create a policy that rejects requests
				regoCode := `package policies.test_reject

main := {
	"rejected": true,
	"rejection_reason": "Test security policy violation"
}`
				policyID = "test-reject-policy"
				displayName := "Test Reject Policy"
				policyType := v1alpha1.GLOBAL
				enabled := true
				priority := int32(100)

				createResp, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policyID,
				}, v1alpha1.Policy{
					DisplayName: &displayName,
					PolicyType:  &policyType,
					RegoCode:    &regoCode,
					Enabled:     &enabled,
					Priority:    &priority,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			})

			AfterEach(func() {
				policyClient.DeletePolicyWithResponse(ctx, policyID)
			})

			It("should return 406 Not Acceptable", func() {
				request := engineapi.EvaluateRequest{
					ServiceInstance: engineapi.ServiceInstance{
						Spec: map[string]any{
							"service_type": "test-service",
						},
					},
				}

				resp, err := engineClient.EvaluateRequestWithResponse(ctx, request)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusNotAcceptable))
				Expect(resp.JSON406).NotTo(BeNil())
				Expect(resp.JSON406.Detail).NotTo(BeNil())
				Expect(*resp.JSON406.Detail).To(ContainSubstring("Test security policy violation"))
			})
		})

		Context("when lower-priority policy violates explicit constraint", func() {
			var policy1ID, policy2ID string

			BeforeEach(func() {
				// Create first policy that sets region with explicit const constraint
				regoCode1 := `package policies.test_constraint1

main := {
	"rejected": false,
	"patch": {
		"region": "us-east-1"
	},
	"constraints": {
		"region": {"const": "us-east-1"}
	},
	"selected_provider": input.provider
}`
				policy1ID = "test-constraint-policy-1"
				displayName1 := "Test Constraint Policy 1"
				policyType1 := v1alpha1.GLOBAL
				enabled1 := true
				priority1 := int32(100)

				createResp1, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy1ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName1,
					PolicyType:  &policyType1,
					RegoCode:    &regoCode1,
					Enabled:     &enabled1,
					Priority:    &priority1,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp1.StatusCode()).To(Equal(http.StatusCreated))

				// Create second policy that tries to change region
				regoCode2 := `package policies.test_constraint2

main := {
	"rejected": false,
	"patch": {
		"region": "us-west-2"
	},
	"selected_provider": input.provider
}`
				policy2ID = "test-constraint-policy-2"
				displayName2 := "Test Constraint Policy 2"
				policyType2 := v1alpha1.GLOBAL
				enabled2 := true
				priority2 := int32(200)

				createResp2, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy2ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName2,
					PolicyType:  &policyType2,
					RegoCode:    &regoCode2,
					Enabled:     &enabled2,
					Priority:    &priority2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp2.StatusCode()).To(Equal(http.StatusCreated))
			})

			AfterEach(func() {
				policyClient.DeletePolicyWithResponse(ctx, policy1ID)
				policyClient.DeletePolicyWithResponse(ctx, policy2ID)
			})

			It("should return 409 Conflict", func() {
				request := engineapi.EvaluateRequest{
					ServiceInstance: engineapi.ServiceInstance{
						Spec: map[string]any{
							"service_type": "test-service",
						},
					},
				}

				resp, err := engineClient.EvaluateRequestWithResponse(ctx, request)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusConflict))
				Expect(resp.JSON409).NotTo(BeNil())
			})
		})

		Context("when lower-priority policy violates range constraint", func() {
			var policy1ID, policy2ID string

			BeforeEach(func() {
				// Create first policy with range constraint
				regoCode1 := `package policies.test_range_constraint1

main := {
	"rejected": false,
	"patch": {
		"cpu_count": 2
	},
	"constraints": {
		"cpu_count": {"minimum": 1, "maximum": 4}
	}
}`
				policy1ID = "test-range-constraint-policy-1"
				displayName1 := "Test Range Constraint Policy 1"
				policyType1 := v1alpha1.GLOBAL
				enabled1 := true
				priority1 := int32(100)

				createResp1, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy1ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName1,
					PolicyType:  &policyType1,
					RegoCode:    &regoCode1,
					Enabled:     &enabled1,
					Priority:    &priority1,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp1.StatusCode()).To(Equal(http.StatusCreated))

				// Second policy sets cpu_count outside the range
				regoCode2 := `package policies.test_range_constraint2

main := {
	"rejected": false,
	"patch": {
		"cpu_count": 16
	}
}`
				policy2ID = "test-range-constraint-policy-2"
				displayName2 := "Test Range Constraint Policy 2"
				policyType2 := v1alpha1.GLOBAL
				enabled2 := true
				priority2 := int32(200)

				createResp2, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy2ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName2,
					PolicyType:  &policyType2,
					RegoCode:    &regoCode2,
					Enabled:     &enabled2,
					Priority:    &priority2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp2.StatusCode()).To(Equal(http.StatusCreated))
			})

			AfterEach(func() {
				policyClient.DeletePolicyWithResponse(ctx, policy1ID)
				policyClient.DeletePolicyWithResponse(ctx, policy2ID)
			})

			It("should return 409 Conflict for out-of-range value", func() {
				request := engineapi.EvaluateRequest{
					ServiceInstance: engineapi.ServiceInstance{
						Spec: map[string]any{
							"service_type": "test-service",
						},
					},
				}

				resp, err := engineClient.EvaluateRequestWithResponse(ctx, request)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusConflict))
				Expect(resp.JSON409).NotTo(BeNil())
			})
		})

		Context("when service provider constraints are violated", func() {
			var policy1ID, policy2ID string

			BeforeEach(func() {
				// First policy constrains providers to aws and gcp
				regoCode1 := `package policies.test_sp_constraint1

main := {
	"rejected": false,
	"service_provider_constraints": {
		"allow_list": ["aws", "gcp"]
	}
}`
				policy1ID = "test-sp-constraint-policy-1"
				displayName1 := "Test SP Constraint Policy 1"
				policyType1 := v1alpha1.GLOBAL
				enabled1 := true
				priority1 := int32(100)

				createResp1, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy1ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName1,
					PolicyType:  &policyType1,
					RegoCode:    &regoCode1,
					Enabled:     &enabled1,
					Priority:    &priority1,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp1.StatusCode()).To(Equal(http.StatusCreated))

				// Second policy selects azure (not in allow list)
				regoCode2 := `package policies.test_sp_constraint2

main := {
	"rejected": false,
	"selected_provider": "azure"
}`
				policy2ID = "test-sp-constraint-policy-2"
				displayName2 := "Test SP Constraint Policy 2"
				policyType2 := v1alpha1.GLOBAL
				enabled2 := true
				priority2 := int32(200)

				createResp2, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy2ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName2,
					PolicyType:  &policyType2,
					RegoCode:    &regoCode2,
					Enabled:     &enabled2,
					Priority:    &priority2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp2.StatusCode()).To(Equal(http.StatusCreated))
			})

			AfterEach(func() {
				policyClient.DeletePolicyWithResponse(ctx, policy1ID)
				policyClient.DeletePolicyWithResponse(ctx, policy2ID)
			})

			It("should return 409 Conflict for disallowed provider", func() {
				request := engineapi.EvaluateRequest{
					ServiceInstance: engineapi.ServiceInstance{
						Spec: map[string]any{
							"service_type": "test-service",
						},
					},
				}

				resp, err := engineClient.EvaluateRequestWithResponse(ctx, request)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusConflict))
				Expect(resp.JSON409).NotTo(BeNil())
			})
		})

		Context("when lower-priority policy sets value within range constraint", func() {
			var policy1ID, policy2ID string

			BeforeEach(func() {
				// First policy sets cpu_count with range constraint
				regoCode1 := `package policies.test_range_success1

main := {
	"rejected": false,
	"patch": {
		"cpu_count": 2
	},
	"constraints": {
		"cpu_count": {"minimum": 1, "maximum": 4}
	}
}`
				policy1ID = "test-range-success-policy-1"
				displayName1 := "Test Range Success Policy 1"
				policyType1 := v1alpha1.GLOBAL
				enabled1 := true
				priority1 := int32(100)

				createResp1, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy1ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName1,
					PolicyType:  &policyType1,
					RegoCode:    &regoCode1,
					Enabled:     &enabled1,
					Priority:    &priority1,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp1.StatusCode()).To(Equal(http.StatusCreated))

				// Second policy sets cpu_count within the range
				regoCode2 := `package policies.test_range_success2

main := {
	"rejected": false,
	"patch": {
		"cpu_count": 3
	}
}`
				policy2ID = "test-range-success-policy-2"
				displayName2 := "Test Range Success Policy 2"
				policyType2 := v1alpha1.GLOBAL
				enabled2 := true
				priority2 := int32(200)

				createResp2, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy2ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName2,
					PolicyType:  &policyType2,
					RegoCode:    &regoCode2,
					Enabled:     &enabled2,
					Priority:    &priority2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp2.StatusCode()).To(Equal(http.StatusCreated))
			})

			AfterEach(func() {
				policyClient.DeletePolicyWithResponse(ctx, policy1ID)
				policyClient.DeletePolicyWithResponse(ctx, policy2ID)
			})

			It("should return MODIFIED with value within range", func() {
				request := engineapi.EvaluateRequest{
					ServiceInstance: engineapi.ServiceInstance{
						Spec: map[string]any{
							"service_type": "test-service",
						},
					},
				}

				resp, err := engineClient.EvaluateRequestWithResponse(ctx, request)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusOK))
				Expect(resp.JSON200).NotTo(BeNil())
				Expect(resp.JSON200.Status).To(Equal(engineapi.MODIFIED))
				Expect(resp.JSON200.EvaluatedServiceInstance.Spec["cpu_count"]).To(BeNumerically("==", 3))
			})
		})

		Context("when lower-priority policy violates enum constraint", func() {
			var policy1ID, policy2ID string

			BeforeEach(func() {
				// First policy sets enum constraint on region
				regoCode1 := `package policies.test_enum_violation1

main := {
	"rejected": false,
	"constraints": {
		"region": {"enum": ["us-east-1", "us-west-2", "eu-west-1"]}
	}
}`
				policy1ID = "test-enum-violation-policy-1"
				displayName1 := "Test Enum Violation Policy 1"
				policyType1 := v1alpha1.GLOBAL
				enabled1 := true
				priority1 := int32(100)

				createResp1, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy1ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName1,
					PolicyType:  &policyType1,
					RegoCode:    &regoCode1,
					Enabled:     &enabled1,
					Priority:    &priority1,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp1.StatusCode()).To(Equal(http.StatusCreated))

				// Second policy sets region not in enum
				regoCode2 := `package policies.test_enum_violation2

main := {
	"rejected": false,
	"patch": {
		"region": "ap-southeast-1"
	}
}`
				policy2ID = "test-enum-violation-policy-2"
				displayName2 := "Test Enum Violation Policy 2"
				policyType2 := v1alpha1.GLOBAL
				enabled2 := true
				priority2 := int32(200)

				createResp2, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy2ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName2,
					PolicyType:  &policyType2,
					RegoCode:    &regoCode2,
					Enabled:     &enabled2,
					Priority:    &priority2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp2.StatusCode()).To(Equal(http.StatusCreated))
			})

			AfterEach(func() {
				policyClient.DeletePolicyWithResponse(ctx, policy1ID)
				policyClient.DeletePolicyWithResponse(ctx, policy2ID)
			})

			It("should return 409 Conflict for value not in enum", func() {
				request := engineapi.EvaluateRequest{
					ServiceInstance: engineapi.ServiceInstance{
						Spec: map[string]any{
							"service_type": "test-service",
						},
					},
				}

				resp, err := engineClient.EvaluateRequestWithResponse(ctx, request)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusConflict))
				Expect(resp.JSON409).NotTo(BeNil())
			})
		})

		Context("when lower-priority policy respects enum constraint", func() {
			var policy1ID, policy2ID string

			BeforeEach(func() {
				// First policy sets enum constraint on region
				regoCode1 := `package policies.test_enum_success1

main := {
	"rejected": false,
	"constraints": {
		"region": {"enum": ["us-east-1", "us-west-2", "eu-west-1"]}
	}
}`
				policy1ID = "test-enum-success-policy-1"
				displayName1 := "Test Enum Success Policy 1"
				policyType1 := v1alpha1.GLOBAL
				enabled1 := true
				priority1 := int32(100)

				createResp1, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy1ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName1,
					PolicyType:  &policyType1,
					RegoCode:    &regoCode1,
					Enabled:     &enabled1,
					Priority:    &priority1,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp1.StatusCode()).To(Equal(http.StatusCreated))

				// Second policy sets region within enum
				regoCode2 := `package policies.test_enum_success2

main := {
	"rejected": false,
	"patch": {
		"region": "us-west-2"
	}
}`
				policy2ID = "test-enum-success-policy-2"
				displayName2 := "Test Enum Success Policy 2"
				policyType2 := v1alpha1.GLOBAL
				enabled2 := true
				priority2 := int32(200)

				createResp2, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy2ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName2,
					PolicyType:  &policyType2,
					RegoCode:    &regoCode2,
					Enabled:     &enabled2,
					Priority:    &priority2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp2.StatusCode()).To(Equal(http.StatusCreated))
			})

			AfterEach(func() {
				policyClient.DeletePolicyWithResponse(ctx, policy1ID)
				policyClient.DeletePolicyWithResponse(ctx, policy2ID)
			})

			It("should return MODIFIED with value from enum", func() {
				request := engineapi.EvaluateRequest{
					ServiceInstance: engineapi.ServiceInstance{
						Spec: map[string]any{
							"service_type": "test-service",
						},
					},
				}

				resp, err := engineClient.EvaluateRequestWithResponse(ctx, request)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusOK))
				Expect(resp.JSON200).NotTo(BeNil())
				Expect(resp.JSON200.Status).To(Equal(engineapi.MODIFIED))
				Expect(resp.JSON200.EvaluatedServiceInstance.Spec["region"]).To(Equal("us-west-2"))
			})
		})

		Context("when constraint-only policy has no patch", func() {
			var policyID string

			BeforeEach(func() {
				// Create a policy with constraints but no patch
				regoCode := `package policies.test_constraint_only

main := {
	"rejected": false,
	"constraints": {
		"cpu_count": {"minimum": 1, "maximum": 8}
	}
}`
				policyID = "test-constraint-only-policy"
				displayName := "Test Constraint Only Policy"
				policyType := v1alpha1.GLOBAL
				enabled := true
				priority := int32(100)

				createResp, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policyID,
				}, v1alpha1.Policy{
					DisplayName: &displayName,
					PolicyType:  &policyType,
					RegoCode:    &regoCode,
					Enabled:     &enabled,
					Priority:    &priority,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			})

			AfterEach(func() {
				policyClient.DeletePolicyWithResponse(ctx, policyID)
			})

			It("should return APPROVED with spec unchanged", func() {
				serviceType := "test-service"
				cpuCount := 4
				request := engineapi.EvaluateRequest{
					ServiceInstance: engineapi.ServiceInstance{
						Spec: map[string]any{
							"service_type": serviceType,
							"cpu_count":    cpuCount,
						},
					},
				}

				resp, err := engineClient.EvaluateRequestWithResponse(ctx, request)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusOK))
				Expect(resp.JSON200).NotTo(BeNil())
				Expect(resp.JSON200.Status).To(Equal(engineapi.APPROVED))
				spec := resp.JSON200.EvaluatedServiceInstance.Spec
				Expect(spec).To(HaveLen(2))
				Expect(spec["service_type"]).To(Equal(serviceType))
				Expect(spec["cpu_count"]).To(BeNumerically("==", cpuCount))
			})
		})

		Context("when lower-priority policy tightens constraints", func() {
			var policy1ID, policy2ID string

			BeforeEach(func() {
				// First policy sets wide range constraint
				regoCode1 := `package policies.test_tighten1

main := {
	"rejected": false,
	"patch": {
		"cpu_count": 5
	},
	"constraints": {
		"cpu_count": {"minimum": 1, "maximum": 10}
	}
}`
				policy1ID = "test-tighten-policy-1"
				displayName1 := "Test Tighten Policy 1"
				policyType1 := v1alpha1.GLOBAL
				enabled1 := true
				priority1 := int32(100)

				createResp1, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy1ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName1,
					PolicyType:  &policyType1,
					RegoCode:    &regoCode1,
					Enabled:     &enabled1,
					Priority:    &priority1,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp1.StatusCode()).To(Equal(http.StatusCreated))

				// Second policy tightens the range and sets value within tightened range
				regoCode2 := `package policies.test_tighten2

main := {
	"rejected": false,
	"patch": {
		"cpu_count": 4
	},
	"constraints": {
		"cpu_count": {"minimum": 2, "maximum": 8}
	}
}`
				policy2ID = "test-tighten-policy-2"
				displayName2 := "Test Tighten Policy 2"
				policyType2 := v1alpha1.GLOBAL
				enabled2 := true
				priority2 := int32(200)

				createResp2, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy2ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName2,
					PolicyType:  &policyType2,
					RegoCode:    &regoCode2,
					Enabled:     &enabled2,
					Priority:    &priority2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp2.StatusCode()).To(Equal(http.StatusCreated))
			})

			AfterEach(func() {
				policyClient.DeletePolicyWithResponse(ctx, policy1ID)
				policyClient.DeletePolicyWithResponse(ctx, policy2ID)
			})

			It("should return MODIFIED with tightened constraints accepted", func() {
				request := engineapi.EvaluateRequest{
					ServiceInstance: engineapi.ServiceInstance{
						Spec: map[string]any{
							"service_type": "test-service",
						},
					},
				}

				resp, err := engineClient.EvaluateRequestWithResponse(ctx, request)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusOK))
				Expect(resp.JSON200).NotTo(BeNil())
				Expect(resp.JSON200.Status).To(Equal(engineapi.MODIFIED))
				Expect(resp.JSON200.EvaluatedServiceInstance.Spec["cpu_count"]).To(BeNumerically("==", 4))
			})
		})

		Context("when lower-priority policy loosens constraints", func() {
			var policy1ID, policy2ID string

			BeforeEach(func() {
				// First policy sets tight range constraint
				regoCode1 := `package policies.test_loosen1

main := {
	"rejected": false,
	"constraints": {
		"cpu_count": {"minimum": 2, "maximum": 4}
	}
}`
				policy1ID = "test-loosen-policy-1"
				displayName1 := "Test Loosen Policy 1"
				policyType1 := v1alpha1.GLOBAL
				enabled1 := true
				priority1 := int32(100)

				createResp1, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy1ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName1,
					PolicyType:  &policyType1,
					RegoCode:    &regoCode1,
					Enabled:     &enabled1,
					Priority:    &priority1,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp1.StatusCode()).To(Equal(http.StatusCreated))

				// Second policy tries to loosen the range
				regoCode2 := `package policies.test_loosen2

main := {
	"rejected": false,
	"constraints": {
		"cpu_count": {"minimum": 1, "maximum": 10}
	}
}`
				policy2ID = "test-loosen-policy-2"
				displayName2 := "Test Loosen Policy 2"
				policyType2 := v1alpha1.GLOBAL
				enabled2 := true
				priority2 := int32(200)

				createResp2, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy2ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName2,
					PolicyType:  &policyType2,
					RegoCode:    &regoCode2,
					Enabled:     &enabled2,
					Priority:    &priority2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp2.StatusCode()).To(Equal(http.StatusCreated))
			})

			AfterEach(func() {
				policyClient.DeletePolicyWithResponse(ctx, policy1ID)
				policyClient.DeletePolicyWithResponse(ctx, policy2ID)
			})

			It("should return 409 Conflict for loosened constraints", func() {
				request := engineapi.EvaluateRequest{
					ServiceInstance: engineapi.ServiceInstance{
						Spec: map[string]any{
							"service_type": "test-service",
						},
					},
				}

				resp, err := engineClient.EvaluateRequestWithResponse(ctx, request)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusConflict))
				Expect(resp.JSON409).NotTo(BeNil())
			})
		})

		Context("when service provider pattern constraint is violated", func() {
			var policy1ID, policy2ID string

			BeforeEach(func() {
				// First policy constrains providers to pattern ^aws
				regoCode1 := `package policies.test_sp_pattern1

main := {
	"rejected": false,
	"service_provider_constraints": {
		"patterns": ["^aws"]
	}
}`
				policy1ID = "test-sp-pattern-policy-1"
				displayName1 := "Test SP Pattern Policy 1"
				policyType1 := v1alpha1.GLOBAL
				enabled1 := true
				priority1 := int32(100)

				createResp1, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy1ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName1,
					PolicyType:  &policyType1,
					RegoCode:    &regoCode1,
					Enabled:     &enabled1,
					Priority:    &priority1,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp1.StatusCode()).To(Equal(http.StatusCreated))

				// Second policy selects gcp (doesn't match ^aws)
				regoCode2 := `package policies.test_sp_pattern2

main := {
	"rejected": false,
	"selected_provider": "gcp"
}`
				policy2ID = "test-sp-pattern-policy-2"
				displayName2 := "Test SP Pattern Policy 2"
				policyType2 := v1alpha1.GLOBAL
				enabled2 := true
				priority2 := int32(200)

				createResp2, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policy2ID,
				}, v1alpha1.Policy{
					DisplayName: &displayName2,
					PolicyType:  &policyType2,
					RegoCode:    &regoCode2,
					Enabled:     &enabled2,
					Priority:    &priority2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp2.StatusCode()).To(Equal(http.StatusCreated))
			})

			AfterEach(func() {
				policyClient.DeletePolicyWithResponse(ctx, policy1ID)
				policyClient.DeletePolicyWithResponse(ctx, policy2ID)
			})

			It("should return 409 Conflict for provider not matching pattern", func() {
				request := engineapi.EvaluateRequest{
					ServiceInstance: engineapi.ServiceInstance{
						Spec: map[string]any{
							"service_type": "test-service",
						},
					},
				}

				resp, err := engineClient.EvaluateRequestWithResponse(ctx, request)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusConflict))
				Expect(resp.JSON409).NotTo(BeNil())
			})
		})

		Context("when label selector matches", func() {
			var policyID string

			BeforeEach(func() {
				// Create a policy with label selector
				regoCode := `package policies.test_labels

main := {
	"rejected": false,
	"patch": {
		"env": "production"
	},
	"selected_provider": "aws"
}`
				policyID = "test-label-policy"
				displayName := "Test Label Policy"
				policyType := v1alpha1.GLOBAL
				enabled := true
				priority := int32(100)
				labelSelector := map[string]string{
					"env":  "prod",
					"team": "backend",
				}

				createResp, err := policyClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{
					Id: &policyID,
				}, v1alpha1.Policy{
					DisplayName:   &displayName,
					PolicyType:    &policyType,
					RegoCode:      &regoCode,
					Enabled:       &enabled,
					Priority:      &priority,
					LabelSelector: &labelSelector,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			})

			AfterEach(func() {
				policyClient.DeletePolicyWithResponse(ctx, policyID)
			})

			It("should apply policy when labels match", func() {
				request := engineapi.EvaluateRequest{
					ServiceInstance: engineapi.ServiceInstance{
						Spec: map[string]any{
							"service_type": "test-service",
							"metadata": map[string]any{
								"labels": map[string]any{
									"env":  "prod",
									"team": "backend",
								},
							},
						},
					},
				}

				resp, err := engineClient.EvaluateRequestWithResponse(ctx, request)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusOK))
				Expect(resp.JSON200.Status).To(Equal(engineapi.MODIFIED))
				Expect(resp.JSON200.EvaluatedServiceInstance.Spec["env"]).To(Equal("production"))
				Expect(resp.JSON200.SelectedProvider).To(Equal("aws"))
			})

			It("should skip policy when labels don't match", func() {
				request := engineapi.EvaluateRequest{
					ServiceInstance: engineapi.ServiceInstance{
						Spec: map[string]any{
							"service_type": "test-service",
							"metadata": map[string]any{
								"labels": map[string]any{
									"env": "dev",
								},
							},
						},
					},
				}

				resp, err := engineClient.EvaluateRequestWithResponse(ctx, request)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusOK))
				Expect(resp.JSON200.Status).To(Equal(engineapi.APPROVED))
				Expect(resp.JSON200.SelectedProvider).To(Equal(""))
			})
		})
	})
})
