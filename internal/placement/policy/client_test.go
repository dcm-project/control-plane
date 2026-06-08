package policy_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/dcm-project/control-plane/internal/placement/policy"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type evaluateResponseFixture struct {
	Status                   string         `json:"status"`
	SelectedProvider         string         `json:"selected_provider"`
	EvaluatedServiceInstance map[string]any `json:"evaluated_service_instance"`
}

var _ = Describe("Policy Engine Client", func() {
	var (
		server     *httptest.Server
		client     policy.Client
		ctx        context.Context
		statusCode int
		response   any
	)

	BeforeEach(func() {
		ctx = context.Background()
		statusCode = http.StatusOK
		response = evaluateResponseFixture{
			Status:           "APPROVED",
			SelectedProvider: "aws",
			EvaluatedServiceInstance: map[string]any{
				"spec": map[string]any{
					"cpu":    "2",
					"memory": "4Gi",
				},
			},
		}

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(statusCode)
			_ = json.NewEncoder(w).Encode(response)
		}))

		var err error
		client, err = policy.NewClient(server.URL, 5*time.Second)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		server.Close()
	})

	Describe("Evaluate", func() {
		It("successfully evaluates a service instance", func() {
			req := policy.EvaluateRequest{
				Spec: map[string]any{
					"cpu":    "2",
					"memory": "4Gi",
				},
			}

			resp, err := client.Evaluate(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Status).To(Equal("APPROVED"))
			Expect(resp.SelectedProvider).To(Equal("aws"))
			Expect(resp.EvaluatedSpec).To(HaveKey("cpu"))
			Expect(resp.EvaluatedSpec).To(HaveKey("memory"))
		})

		It("handles MODIFIED status", func() {
			response = evaluateResponseFixture{
				Status:           "MODIFIED",
				SelectedProvider: "gcp",
				EvaluatedServiceInstance: map[string]any{
					"spec": map[string]any{
						"cpu":    "4",
						"memory": "8Gi",
					},
				},
			}

			req := policy.EvaluateRequest{
				Spec: map[string]any{
					"cpu":    "2",
					"memory": "4Gi",
				},
			}

			resp, err := client.Evaluate(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Status).To(Equal("MODIFIED"))
			Expect(resp.SelectedProvider).To(Equal("gcp"))
			Expect(resp.EvaluatedSpec["cpu"]).To(Equal("4"))
			Expect(resp.EvaluatedSpec["memory"]).To(Equal("8Gi"))
		})

		It("returns error for non-200 response", func() {
			statusCode = http.StatusBadRequest
			response = map[string]any{
				"type":   "bad-request",
				"title":  "Bad Request",
				"status": 400,
			}

			req := policy.EvaluateRequest{
				Spec: map[string]any{"cpu": "invalid"},
			}

			_, err := client.Evaluate(ctx, req)

			Expect(err).To(HaveOccurred())

			var httpErr *policy.HTTPError
			Expect(errors.As(err, &httpErr)).To(BeTrue())
			Expect(httpErr.StatusCode).To(Equal(400))
			Expect(httpErr.Body).To(ContainSubstring("bad-request"))
			Expect(httpErr.Body).To(ContainSubstring("Bad Request"))
		})

		It("returns HTTPError that can be extracted with errors.As", func() {
			statusCode = http.StatusNotAcceptable
			response = map[string]any{
				"type":   "policy-rejected",
				"title":  "Policy Rejected",
				"status": 406,
			}

			req := policy.EvaluateRequest{
				Spec: map[string]any{"cpu": "100"},
			}

			_, err := client.Evaluate(ctx, req)

			Expect(err).To(HaveOccurred())

			var httpErr *policy.HTTPError
			Expect(errors.As(err, &httpErr)).To(BeTrue(), "errors.As should be able to extract policy.HTTPError")
			Expect(httpErr).To(HaveOccurred())
			Expect(httpErr.StatusCode).To(Equal(406))
			Expect(httpErr.Body).To(ContainSubstring("policy-rejected"))
		})

		It("returns HTTPError for 500 internal server error", func() {
			statusCode = http.StatusInternalServerError
			response = map[string]any{
				"type":   "internal-error",
				"title":  "Internal Server Error",
				"status": 500,
			}

			req := policy.EvaluateRequest{
				Spec: map[string]any{"cpu": "2"},
			}

			_, err := client.Evaluate(ctx, req)

			Expect(err).To(HaveOccurred())

			var httpErr *policy.HTTPError
			Expect(errors.As(err, &httpErr)).To(BeTrue())
			Expect(httpErr.StatusCode).To(Equal(500))
		})
	})

	Describe("NewClient", func() {
		It("creates client with valid base URL", func() {
			client, err := policy.NewClient("http://localhost:8080", 5*time.Second)

			Expect(err).NotTo(HaveOccurred())
			Expect(client).NotTo(BeNil())
		})

		It("enforces configured HTTP timeout", func() {
			slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				time.Sleep(50 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
			}))
			defer slowServer.Close()

			c, err := policy.NewClient(slowServer.URL, 10*time.Millisecond)
			Expect(err).NotTo(HaveOccurred())

			_, err = c.Evaluate(ctx, policy.EvaluateRequest{Spec: map[string]any{"cpu": "2"}})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Client.Timeout"))
		})
	})
})
