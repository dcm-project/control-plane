//go:build subsystem

package subsystem_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/control-plane/pkg/policy/client"
)

var (
	apiClient *client.ClientWithResponses
	ctx       context.Context
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

var _ = BeforeSuite(func() {
	ctx = context.Background()

	// Get API URL from environment or use default
	apiURL := getEnvOrDefault("API_URL", "http://localhost:8080/api/v1alpha1")

	// Initialize API client
	var err error
	apiClient, err = client.NewClientWithResponses(apiURL)
	Expect(err).NotTo(HaveOccurred(), "Failed to create API client")

	// Verify health endpoint
	Eventually(func() error {
		resp, err := http.Get(apiURL + "/health")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("health check returned %d", resp.StatusCode)
		}
		return nil
	}, 30*time.Second, 1*time.Second).Should(Succeed(), "Service should be healthy")

	// Get Engine API URL from environment or use default
	engineURL := getEnvOrDefault("ENGINE_API_URL", "http://localhost:8081/api/v1alpha1")

	// Basic connectivity check for engine API
	Eventually(func() error {
		resp, err := http.Get(engineURL + "/policies:evaluateRequest")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		// Expect any response (stub returns 501)
		if resp.StatusCode == 0 {
			return fmt.Errorf("no response from engine API")
		}
		return nil
	}, 10*time.Second, 1*time.Second).Should(Succeed(), "Engine API should be reachable")
})

// Helper functions

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func ptr[T any](v T) *T {
	return &v
}
