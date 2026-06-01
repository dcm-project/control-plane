package engine

import (
	"testing"

	engineapi "github.com/dcm-project/control-plane/api/policy/v1alpha1/engine"
	engineserver "github.com/dcm-project/control-plane/internal/policy/api/engine"
	"github.com/dcm-project/control-plane/internal/policy/service"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestEngine(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Engine Handlers Suite")
}

var _ = Describe("extractRequestLabels", func() {
	It("returns error when service_type is missing", func() {
		spec := map[string]any{}
		labels, err := extractRequestLabels(spec)
		Expect(err).To(MatchError("service type is required"))
		Expect(labels).To(BeNil())
	})

	It("returns error when service_type is not a string", func() {
		spec := map[string]any{"service_type": 123}
		labels, err := extractRequestLabels(spec)
		Expect(err).To(MatchError("service type is required"))
		Expect(labels).To(BeNil())
	})

	It("returns only service_type when no metadata or labels", func() {
		spec := map[string]any{"service_type": "compute"}
		labels, err := extractRequestLabels(spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(labels).To(Equal(map[string]string{"service_type": "compute"}))
	})

	It("includes metadata.labels when present", func() {
		spec := map[string]any{
			"service_type": "storage",
			"metadata": map[string]any{
				"labels": map[string]any{
					"env":    "prod",
					"region": "us-east-1",
				},
			},
		}
		labels, err := extractRequestLabels(spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(labels).To(Equal(map[string]string{
			"service_type": "storage",
			"env":          "prod",
			"region":       "us-east-1",
		}))
	})

	It("skips non-string label values", func() {
		spec := map[string]any{
			"service_type": "compute",
			"metadata": map[string]any{
				"labels": map[string]any{
					"env":  "prod",
					"num":  42,
					"flag": true,
				},
			},
		}
		labels, err := extractRequestLabels(spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(labels).To(Equal(map[string]string{
			"service_type": "compute",
			"env":          "prod",
		}))
	})

	It("returns only service_type when metadata is not a map", func() {
		spec := map[string]any{
			"service_type": "compute",
			"metadata":     "not-a-map",
		}
		labels, err := extractRequestLabels(spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(labels).To(Equal(map[string]string{"service_type": "compute"}))
	})

	It("returns only service_type when labels is not a map", func() {
		spec := map[string]any{
			"service_type": "compute",
			"metadata": map[string]any{
				"labels": []string{"a", "b"},
			},
		}
		labels, err := extractRequestLabels(spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(labels).To(Equal(map[string]string{"service_type": "compute"}))
	})
})

var _ = Describe("toServiceEvaluationRequest", func() {
	It("converts valid request to service evaluation request", func() {
		spec := map[string]any{
			"service_type": "compute",
			"metadata": map[string]any{
				"labels": map[string]any{
					"env": "prod",
				},
			},
		}
		req := engineserver.EvaluateRequestRequestObject{
			Body: &engineapi.EvaluateRequest{
				ServiceInstance: engineapi.ServiceInstance{Spec: spec},
			},
		}
		got, err := toServiceEvaluationRequest(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
		Expect(got.ServiceInstance).To(Equal(spec))
		Expect(got.RequestLabels).To(Equal(map[string]string{
			"service_type": "compute",
			"env":          "prod",
		}))
	})

	It("returns error when spec has no service_type", func() {
		spec := map[string]any{"other": "value"}
		req := engineserver.EvaluateRequestRequestObject{
			Body: &engineapi.EvaluateRequest{
				ServiceInstance: engineapi.ServiceInstance{Spec: spec},
			},
		}
		got, err := toServiceEvaluationRequest(req)
		Expect(err).To(MatchError("service type is required"))
		Expect(got).To(BeNil())
	})
})

var _ = Describe("toEngineEvaluationResponse", func() {
	It("converts service response to engine evaluation response", func() {
		spec := map[string]any{"service_type": "compute", "provider": "acme"}
		resp := &service.EvaluationResponse{
			EvaluatedServiceInstance: spec,
			SelectedProvider:         "acme",
			Status:                   service.EvaluationStatusApproved,
		}
		got := toEngineEvaluationResponse(resp)
		Expect(got.EvaluatedServiceInstance.Spec).To(Equal(spec))
		Expect(got.SelectedProvider).To(Equal("acme"))
		Expect(got.Status).To(Equal(engineapi.APPROVED))
	})

	It("maps MODIFIED status correctly", func() {
		resp := &service.EvaluationResponse{
			EvaluatedServiceInstance: map[string]any{"service_type": "storage"},
			SelectedProvider:         "other",
			Status:                   service.EvaluationStatusModified,
		}
		got := toEngineEvaluationResponse(resp)
		Expect(got.Status).To(Equal(engineapi.MODIFIED))
		Expect(got.SelectedProvider).To(Equal("other"))
	})
})
