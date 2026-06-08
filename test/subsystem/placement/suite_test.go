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

	"github.com/dcm-project/control-plane/pkg/placement/client"
)

var (
	apiClient         *client.ClientWithResponses
	policyWireMockURL string
	sprmWireMockURL   string
	httpClient        = &http.Client{Timeout: 10 * time.Second}
)

func TestSubsystem(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Subsystem Suite")
}

var _ = BeforeSuite(func() {
	serviceURL := envOrDefault("PLACEMENT_MANAGER_URL", "http://localhost:28080")
	policyWireMockURL = envOrDefault("POLICY_MANAGER_EVALUATION_URL", "http://localhost:28081")
	sprmWireMockURL = envOrDefault("SP_RESOURCE_MANAGER_URL", "http://localhost:28082")

	var err error
	apiClient, err = client.NewClientWithResponses(serviceURL + "/api/v1alpha1")
	Expect(err).NotTo(HaveOccurred())

	Eventually(func() error {
		resp, err := http.Get(serviceURL + "/api/v1alpha1/health")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode() != http.StatusOK {
			return fmt.Errorf("health check returned %d", resp.StatusCode())
		}
		return nil
	}).WithTimeout(60 * time.Second).WithPolling(2 * time.Second).Should(Succeed())
})

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
