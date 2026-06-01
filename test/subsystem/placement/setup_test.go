//go:build subsystem

package subsystem_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	. "github.com/onsi/gomega"
)

// --- Policy WireMock helpers ---

func resetPolicyWireMock() {
	resetWireMock(policyWireMockURL)
}

func stubPolicyEvaluateApproved(providerName string) {
	stub := map[string]any{
		"request": map[string]any{
			"method":     "POST",
			"urlPattern": "/api/v1alpha1/policies:evaluateRequest",
		},
		"response": map[string]any{
			"status": 200,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"status":            "APPROVED",
				"selected_provider": providerName,
				"evaluated_service_instance": map[string]any{
					"spec": map[string]any{"cpu": 2, "memory": "4Gi"},
				},
			},
		},
	}
	postWireMockMapping(policyWireMockURL, stub)
}

func stubPolicyEvaluateModified(providerName string, evaluatedSpec map[string]any) {
	stub := map[string]any{
		"request": map[string]any{
			"method":     "POST",
			"urlPattern": "/api/v1alpha1/policies:evaluateRequest",
		},
		"response": map[string]any{
			"status": 200,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"status":            "MODIFIED",
				"selected_provider": providerName,
				"evaluated_service_instance": map[string]any{
					"spec": evaluatedSpec,
				},
			},
		},
	}
	postWireMockMapping(policyWireMockURL, stub)
}

func stubPolicyEvaluateRejected() {
	stub := map[string]any{
		"request": map[string]any{
			"method":     "POST",
			"urlPattern": "/api/v1alpha1/policies:evaluateRequest",
		},
		"response": map[string]any{
			"status": 406,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"type":   "policy-rejected",
				"title":  "Policy Rejected",
				"status": 406,
			},
		},
	}
	postWireMockMapping(policyWireMockURL, stub)
}

func stubPolicyEvaluateFailure() {
	stub := map[string]any{
		"request": map[string]any{
			"method":     "POST",
			"urlPattern": "/api/v1alpha1/policies:evaluateRequest",
		},
		"response": map[string]any{
			"status": 500,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"type":   "internal-error",
				"title":  "Internal Server Error",
				"status": 500,
			},
		},
	}
	postWireMockMapping(policyWireMockURL, stub)
}

func verifyPolicyEvaluateCalled(expectedCount int) {
	verifyWireMockRequestCount(policyWireMockURL, "POST", "/api/v1alpha1/policies:evaluateRequest", expectedCount)
}

// --- SPRM WireMock helpers ---

func resetSPRMWireMock() {
	resetWireMock(sprmWireMockURL)
}

func stubSPRMCreateResource() {
	stub := map[string]any{
		"request": map[string]any{
			"method":     "POST",
			"urlPattern": "/api/v1alpha1/service-type-instances.*",
		},
		"response": map[string]any{
			"status": 201,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"id":            "sprm-instance-1",
				"status":        "provisioning",
				"provider_name": "test-provider",
				"spec":          map[string]any{"cpu": 2, "memory": "4Gi"},
			},
		},
	}
	postWireMockMapping(sprmWireMockURL, stub)
}

func stubSPRMCreateResourceFailure() {
	stub := map[string]any{
		"request": map[string]any{
			"method":     "POST",
			"urlPattern": "/api/v1alpha1/service-type-instances.*",
		},
		"response": map[string]any{
			"status": 500,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"type":   "internal-error",
				"title":  "Internal Server Error",
				"status": 500,
			},
		},
	}
	postWireMockMapping(sprmWireMockURL, stub)
}

func stubSPRMDeleteResource() {
	stub := map[string]any{
		"request": map[string]any{
			"method":         "DELETE",
			"urlPathPattern": "/api/v1alpha1/service-type-instances/.*",
		},
		"response": map[string]any{
			"status": 204,
		},
	}
	postWireMockMapping(sprmWireMockURL, stub)
}

func stubSPRMDeleteResourceFailure() {
	stub := map[string]any{
		"request": map[string]any{
			"method":         "DELETE",
			"urlPathPattern": "/api/v1alpha1/service-type-instances/.*",
		},
		"response": map[string]any{
			"status": 500,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"type":   "internal-error",
				"title":  "Internal Server Error",
				"status": 500,
			},
		},
	}
	postWireMockMapping(sprmWireMockURL, stub)
}

func verifySPRMCreateResourceCalled(expectedCount int) {
	verifyWireMockRequestCount(sprmWireMockURL, "POST", "/api/v1alpha1/service-type-instances", expectedCount)
}

func verifySPRMDeleteResourceCalled(expectedCount int) {
	verifyWireMockRequestCount(sprmWireMockURL, "DELETE", "/api/v1alpha1/service-type-instances/.*", expectedCount)
}

func stubSPRMDeleteResourceDeferred() {
	stub := map[string]any{
		"request": map[string]any{
			"method":         "DELETE",
			"urlPathPattern": "/api/v1alpha1/service-type-instances/.*",
			"queryParameters": map[string]any{
				"deferred": map[string]any{
					"equalTo": "true",
				},
			},
		},
		"response": map[string]any{
			"status": 204,
		},
	}
	postWireMockMapping(sprmWireMockURL, stub)
}

func stubSPRMDeleteResourceDeferredFailure() {
	stub := map[string]any{
		"request": map[string]any{
			"method":         "DELETE",
			"urlPathPattern": "/api/v1alpha1/service-type-instances/.*",
			"queryParameters": map[string]any{
				"deferred": map[string]any{
					"equalTo": "true",
				},
			},
		},
		"response": map[string]any{
			"status": 500,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"type":   "internal-error",
				"title":  "Internal Server Error",
				"status": 500,
			},
		},
	}
	postWireMockMapping(sprmWireMockURL, stub)
}

func verifySPRMDeleteResourceDeferredCalled(expectedCount int) {
	body := map[string]any{
		"method":         "DELETE",
		"urlPathPattern": "/api/v1alpha1/service-type-instances/.*",
		"queryParameters": map[string]any{
			"deferred": map[string]any{
				"equalTo": "true",
			},
		},
	}
	data, err := json.Marshal(body)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	req, err := http.NewRequest(http.MethodPost, sprmWireMockURL+"/__admin/requests/count", bytes.NewReader(data))
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	var result map[string]any
	ExpectWithOffset(1, json.NewDecoder(resp.Body).Decode(&result)).To(Succeed())
	ExpectWithOffset(1, int(result["count"].(float64))).To(Equal(expectedCount))
}

// --- Generic WireMock helpers ---

func resetWireMock(baseURL string) {
	req, err := http.NewRequest(http.MethodPost, baseURL+"/__admin/reset", nil)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	resp, err := httpClient.Do(req)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	ExpectWithOffset(1, resp.StatusCode).To(Equal(http.StatusOK))
}

func postWireMockMapping(baseURL string, stub map[string]any) {
	data, err := json.Marshal(stub)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())

	req, err := http.NewRequest(http.MethodPost, baseURL+"/__admin/mappings", bytes.NewReader(data))
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	ExpectWithOffset(2, resp.StatusCode).To(Equal(http.StatusCreated), fmt.Sprintf("WireMock stub creation failed: %s", string(body)))
}

func verifyWireMockRequestCount(baseURL, method, urlPattern string, expectedCount int) {
	body := map[string]any{
		"method":     method,
		"urlPattern": urlPattern + ".*",
	}
	data, err := json.Marshal(body)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())

	req, err := http.NewRequest(http.MethodPost, baseURL+"/__admin/requests/count", bytes.NewReader(data))
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	var result map[string]any
	ExpectWithOffset(2, json.NewDecoder(resp.Body).Decode(&result)).To(Succeed())
	ExpectWithOffset(2, int(result["count"].(float64))).To(Equal(expectedCount))
}
