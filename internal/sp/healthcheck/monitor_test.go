package healthcheck_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/dcm-project/control-plane/internal/sp/config"
	"github.com/dcm-project/control-plane/internal/sp/healthcheck"
	"github.com/dcm-project/control-plane/internal/sp/store/model"
	providerstore "github.com/dcm-project/control-plane/internal/sp/store/provider"
	rmstore "github.com/dcm-project/control-plane/internal/sp/store/resource_manager"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// testHealthCheckConfig returns a default config for testing
func testHealthCheckConfig() *config.HealthCheckConfig {
	return &config.HealthCheckConfig{
		Interval:               10 * time.Second,
		Timeout:                5 * time.Second,
		MaxConsecutiveFailures: 3,
		BaseBackoffInterval:    10 * time.Second,
		MaxBackoffInterval:     5 * time.Minute,
	}
}

// mockProviderStore implements store.Provider interface for testing
type mockProviderStore struct {
	providers           model.ProviderList
	healthStatusUpdates []healthStatusUpdate
}

type healthStatusUpdate struct {
	ID                  string
	Status              model.HealthStatus
	ConsecutiveFailures int
	NextCheck           time.Time
}

func (m *mockProviderStore) ListProvidersForHealthCheck(_ context.Context, now time.Time) (model.ProviderList, error) {
	var result model.ProviderList
	for _, p := range m.providers {
		if p.NextHealthCheck == nil || !p.NextHealthCheck.After(now) {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *mockProviderStore) UpdateHealthStatus(_ context.Context, id string, status model.HealthStatus, consecutiveFailures int, nextCheck time.Time) error {
	m.healthStatusUpdates = append(m.healthStatusUpdates, healthStatusUpdate{
		ID:                  id,
		Status:              status,
		ConsecutiveFailures: consecutiveFailures,
		NextCheck:           nextCheck,
	})
	return nil
}

func (m *mockProviderStore) List(_ context.Context, _ *providerstore.ProviderFilter, _ *providerstore.Pagination) (model.ProviderList, error) {
	return m.providers, nil
}

func (m *mockProviderStore) Count(_ context.Context, _ *providerstore.ProviderFilter) (int64, error) {
	return int64(len(m.providers)), nil
}

func (m *mockProviderStore) ExistsByID(_ context.Context, id string) (bool, error) {
	for _, p := range m.providers {
		if p.ID == id {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockProviderStore) Create(_ context.Context, provider model.Provider) (*model.Provider, error) {
	return &provider, nil
}

func (m *mockProviderStore) Delete(_ context.Context, _ string) error {
	return nil
}

func (m *mockProviderStore) Update(_ context.Context, provider model.Provider) (*model.Provider, error) {
	return &provider, nil
}

func (m *mockProviderStore) Get(_ context.Context, id string) (*model.Provider, error) {
	for _, p := range m.providers {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, nil
}

func (m *mockProviderStore) GetByName(_ context.Context, name string) (*model.Provider, error) {
	for _, p := range m.providers {
		if p.Name == name {
			return &p, nil
		}
	}
	return nil, nil
}

// mockInstanceStore implements rmstore.ServiceTypeInstance for testing
type mockInstanceStore struct {
	markProviderDeletionsCalls []string
	reactivateProviderCalls    []string
	markPendingProviderCalls   []string
	markPendingProviderResult  bool
	markPendingProviderErr     error
}

func (m *mockInstanceStore) MarkProviderDeletionsPendingProvider(_ context.Context, providerName string) error {
	m.markProviderDeletionsCalls = append(m.markProviderDeletionsCalls, providerName)
	return nil
}

func (m *mockInstanceStore) ReactivateProviderDeletions(_ context.Context, providerName string) error {
	m.reactivateProviderCalls = append(m.reactivateProviderCalls, providerName)
	return nil
}

func (m *mockInstanceStore) MarkPendingProviderIfNotReady(_ context.Context, instanceID string) (bool, error) {
	m.markPendingProviderCalls = append(m.markPendingProviderCalls, instanceID)
	return m.markPendingProviderResult, m.markPendingProviderErr
}

// Unused interface methods
func (m *mockInstanceStore) List(_ context.Context, _ *rmstore.ServiceTypeInstanceListOptions) (*rmstore.ServiceTypeInstanceListResult, error) {
	return nil, nil
}

func (m *mockInstanceStore) Create(_ context.Context, inst model.ServiceTypeInstance) (*model.ServiceTypeInstance, error) {
	return &inst, nil
}

func (m *mockInstanceStore) Get(_ context.Context, _ string, _ bool) (*model.ServiceTypeInstance, error) {
	return nil, nil
}

func (m *mockInstanceStore) ExistsByID(_ context.Context, _ string) (bool, error) { return false, nil }

func (m *mockInstanceStore) UpdateStatus(_ context.Context, _ string, _ string, _ string) error {
	return nil
}

func (m *mockInstanceStore) UpdateStatusIfPending(_ context.Context, _ string, _ string, _ string, _ string) (bool, error) {
	return false, nil
}

func (m *mockInstanceStore) MarkForDeletion(_ context.Context, _ string) error { return nil }

func (m *mockInstanceStore) ListPendingDeletions(_ context.Context) ([]model.ServiceTypeInstance, error) {
	return nil, nil
}

func (m *mockInstanceStore) IncrementDeletionRetry(_ context.Context, _ string) error { return nil }
func (m *mockInstanceStore) MarkDeletionFailed(_ context.Context, _ string) error     { return nil }
func (m *mockInstanceStore) HardDelete(_ context.Context, _ string) error             { return nil }
func (m *mockInstanceStore) ResetRetryCount(_ context.Context, _ string) error        { return nil }

func healthyServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":"healthy"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func unhealthyServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"unhealthy"}`)
	}))
}

func failingServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
}

var _ = Describe("Monitor", func() {
	var (
		cfg     *config.HealthCheckConfig
		monitor *healthcheck.Monitor
		ctx     context.Context
	)

	BeforeEach(func() {
		cfg = testHealthCheckConfig()
		ctx = context.Background()
	})

	Describe("CalculateNextCheckTime", func() {
		Context("for a Ready provider", func() {
			It("schedules next check at the configured interval", func() {
				mockStore := &mockProviderStore{}
				monitor = healthcheck.NewMonitor(mockStore, &mockInstanceStore{}, cfg)
				now := time.Now()

				nextCheck := monitor.CalculateNextCheckTime(now, model.HealthStatusReady, 0)

				Expect(nextCheck.Sub(now)).To(Equal(cfg.Interval))
			})
		})

		Context("for an Unhealthy provider", func() {
			It("schedules next check at the configured interval", func() {
				mockStore := &mockProviderStore{}
				monitor = healthcheck.NewMonitor(mockStore, &mockInstanceStore{}, cfg)
				now := time.Now()

				nextCheck := monitor.CalculateNextCheckTime(now, model.HealthStatusUnhealthy, 0)

				Expect(nextCheck.Sub(now)).To(Equal(cfg.Interval))
			})
		})

		Context("for an Unavailable provider with exponential backoff", func() {
			var (
				mockStore *mockProviderStore
				now       time.Time
			)

			BeforeEach(func() {
				mockStore = &mockProviderStore{}
				monitor = healthcheck.NewMonitor(mockStore, &mockInstanceStore{}, cfg)
				now = time.Now()
			})

			It("uses base backoff interval when just became Unavailable (3 failures)", func() {
				nextCheck := monitor.CalculateNextCheckTime(now, model.HealthStatusUnavailable, 3)
				Expect(nextCheck.Sub(now)).To(Equal(cfg.BaseBackoffInterval))
			})

			It("doubles backoff for 4 consecutive failures", func() {
				nextCheck := monitor.CalculateNextCheckTime(now, model.HealthStatusUnavailable, 4)
				Expect(nextCheck.Sub(now)).To(Equal(cfg.BaseBackoffInterval * 2))
			})

			It("quadruples backoff for 5 consecutive failures", func() {
				nextCheck := monitor.CalculateNextCheckTime(now, model.HealthStatusUnavailable, 5)
				Expect(nextCheck.Sub(now)).To(Equal(cfg.BaseBackoffInterval * 4))
			})

			It("caps backoff at max interval for many failures", func() {
				nextCheck := monitor.CalculateNextCheckTime(now, model.HealthStatusUnavailable, 100)
				Expect(nextCheck.Sub(now)).To(Equal(cfg.MaxBackoffInterval))
			})
		})
	})

	Describe("CheckProviders", func() {
		Context("with a healthy provider", func() {
			It("sets status to Ready with zero consecutive failures", func() {
				server := healthyServer()
				defer server.Close()

				providerID := uuid.New().String()
				mockStore := &mockProviderStore{
					providers: model.ProviderList{
						{
							ID:           providerID,
							Name:         "test-provider",
							Endpoint:     server.URL,
							HealthStatus: model.HealthStatusReady,
						},
					},
				}

				monitor = healthcheck.NewMonitor(mockStore, &mockInstanceStore{}, cfg)
				monitor.CheckProviders(ctx)

				Expect(mockStore.healthStatusUpdates).To(HaveLen(1))
				update := mockStore.healthStatusUpdates[0]
				Expect(update.Status).To(Equal(model.HealthStatusReady))
				Expect(update.ConsecutiveFailures).To(Equal(0))
			})
		})

		Context("with a provider reporting unhealthy backing provider", func() {
			It("transitions to Unhealthy immediately", func() {
				server := unhealthyServer()
				defer server.Close()

				providerID := uuid.New().String()
				mockStore := &mockProviderStore{
					providers: model.ProviderList{
						{
							ID:           providerID,
							Name:         "test-provider",
							Endpoint:     server.URL,
							HealthStatus: model.HealthStatusReady,
						},
					},
				}

				monitor = healthcheck.NewMonitor(mockStore, &mockInstanceStore{}, cfg)
				monitor.CheckProviders(ctx)

				Expect(mockStore.healthStatusUpdates).To(HaveLen(1))
				update := mockStore.healthStatusUpdates[0]
				Expect(update.Status).To(Equal(model.HealthStatusUnhealthy))
				Expect(update.ConsecutiveFailures).To(Equal(0))
			})
		})

		Context("with a failing provider", func() {
			It("becomes Unavailable after reaching max consecutive failures", func() {
				server := failingServer()
				defer server.Close()

				providerID := uuid.New().String()
				mockStore := &mockProviderStore{
					providers: model.ProviderList{
						{
							ID:                  providerID,
							Name:                "test-provider",
							Endpoint:            server.URL,
							HealthStatus:        model.HealthStatusReady,
							ConsecutiveFailures: 2,
						},
					},
				}

				monitor = healthcheck.NewMonitor(mockStore, &mockInstanceStore{}, cfg)
				monitor.CheckProviders(ctx)

				Expect(mockStore.healthStatusUpdates).To(HaveLen(1))
				update := mockStore.healthStatusUpdates[0]
				Expect(update.Status).To(Equal(model.HealthStatusUnavailable))
				Expect(update.ConsecutiveFailures).To(Equal(3))
			})

			It("stays Ready until reaching max consecutive failures", func() {
				server := failingServer()
				defer server.Close()

				providerID := uuid.New().String()
				mockStore := &mockProviderStore{
					providers: model.ProviderList{
						{
							ID:                  providerID,
							Name:                "test-provider",
							Endpoint:            server.URL,
							HealthStatus:        model.HealthStatusReady,
							ConsecutiveFailures: 1,
						},
					},
				}

				monitor = healthcheck.NewMonitor(mockStore, &mockInstanceStore{}, cfg)
				monitor.CheckProviders(ctx)

				Expect(mockStore.healthStatusUpdates).To(HaveLen(1))
				update := mockStore.healthStatusUpdates[0]
				Expect(update.Status).To(Equal(model.HealthStatusReady))
				Expect(update.ConsecutiveFailures).To(Equal(2))
			})
		})

		Context("with a recovered provider", func() {
			It("resets Unavailable provider to Ready with zero consecutive failures", func() {
				server := healthyServer()
				defer server.Close()

				providerID := uuid.New().String()
				mockStore := &mockProviderStore{
					providers: model.ProviderList{
						{
							ID:                  providerID,
							Name:                "test-provider",
							Endpoint:            server.URL,
							HealthStatus:        model.HealthStatusUnavailable,
							ConsecutiveFailures: 5,
						},
					},
				}

				monitor = healthcheck.NewMonitor(mockStore, &mockInstanceStore{}, cfg)
				monitor.CheckProviders(ctx)

				Expect(mockStore.healthStatusUpdates).To(HaveLen(1))
				update := mockStore.healthStatusUpdates[0]
				Expect(update.Status).To(Equal(model.HealthStatusReady))
				Expect(update.ConsecutiveFailures).To(Equal(0))
			})

			It("resets Unhealthy provider to Ready with zero consecutive failures", func() {
				server := healthyServer()
				defer server.Close()

				providerID := uuid.New().String()
				mockStore := &mockProviderStore{
					providers: model.ProviderList{
						{
							ID:           providerID,
							Name:         "test-provider",
							Endpoint:     server.URL,
							HealthStatus: model.HealthStatusUnhealthy,
						},
					},
				}

				monitor = healthcheck.NewMonitor(mockStore, &mockInstanceStore{}, cfg)
				monitor.CheckProviders(ctx)

				Expect(mockStore.healthStatusUpdates).To(HaveLen(1))
				update := mockStore.healthStatusUpdates[0]
				Expect(update.Status).To(Equal(model.HealthStatusReady))
				Expect(update.ConsecutiveFailures).To(Equal(0))
			})
		})

		Context("deletion status transitions on provider health change", func() {
			It("calls MarkProviderDeletionsPendingProvider when provider transitions Ready -> Unavailable", func() {
				server := failingServer()
				defer server.Close()

				providerID := uuid.New().String()
				mockStore := &mockProviderStore{
					providers: model.ProviderList{
						{
							ID:                  providerID,
							Name:                "failing-provider",
							Endpoint:            server.URL,
							HealthStatus:        model.HealthStatusReady,
							ConsecutiveFailures: 2,
						},
					},
				}

				instStore := &mockInstanceStore{}
				monitor = healthcheck.NewMonitor(mockStore, instStore, cfg)
				monitor.CheckProviders(ctx)

				Expect(instStore.markProviderDeletionsCalls).To(HaveLen(1))
				Expect(instStore.markProviderDeletionsCalls[0]).To(Equal("failing-provider"))
				Expect(instStore.reactivateProviderCalls).To(BeEmpty())
			})

			It("calls MarkProviderDeletionsPendingProvider when provider transitions Ready -> Unhealthy", func() {
				server := unhealthyServer()
				defer server.Close()

				providerID := uuid.New().String()
				mockStore := &mockProviderStore{
					providers: model.ProviderList{
						{
							ID:           providerID,
							Name:         "unhealthy-provider",
							Endpoint:     server.URL,
							HealthStatus: model.HealthStatusReady,
						},
					},
				}

				instStore := &mockInstanceStore{}
				monitor = healthcheck.NewMonitor(mockStore, instStore, cfg)
				monitor.CheckProviders(ctx)

				Expect(instStore.markProviderDeletionsCalls).To(HaveLen(1))
				Expect(instStore.markProviderDeletionsCalls[0]).To(Equal("unhealthy-provider"))
				Expect(instStore.reactivateProviderCalls).To(BeEmpty())
			})

			It("calls ReactivateProviderDeletions when provider transitions Unavailable -> Ready", func() {
				server := healthyServer()
				defer server.Close()

				providerID := uuid.New().String()
				mockStore := &mockProviderStore{
					providers: model.ProviderList{
						{
							ID:                  providerID,
							Name:                "recovered-provider",
							Endpoint:            server.URL,
							HealthStatus:        model.HealthStatusUnavailable,
							ConsecutiveFailures: 5,
						},
					},
				}

				instStore := &mockInstanceStore{}
				monitor = healthcheck.NewMonitor(mockStore, instStore, cfg)
				monitor.CheckProviders(ctx)

				Expect(instStore.reactivateProviderCalls).To(HaveLen(1))
				Expect(instStore.reactivateProviderCalls[0]).To(Equal("recovered-provider"))
				Expect(instStore.markProviderDeletionsCalls).To(BeEmpty())
			})

			It("calls ReactivateProviderDeletions when provider transitions Unhealthy -> Ready", func() {
				server := healthyServer()
				defer server.Close()

				providerID := uuid.New().String()
				mockStore := &mockProviderStore{
					providers: model.ProviderList{
						{
							ID:           providerID,
							Name:         "recovered-provider",
							Endpoint:     server.URL,
							HealthStatus: model.HealthStatusUnhealthy,
						},
					},
				}

				instStore := &mockInstanceStore{}
				monitor = healthcheck.NewMonitor(mockStore, instStore, cfg)
				monitor.CheckProviders(ctx)

				Expect(instStore.reactivateProviderCalls).To(HaveLen(1))
				Expect(instStore.reactivateProviderCalls[0]).To(Equal("recovered-provider"))
				Expect(instStore.markProviderDeletionsCalls).To(BeEmpty())
			})

			It("does not call either method when provider stays Ready", func() {
				server := healthyServer()
				defer server.Close()

				providerID := uuid.New().String()
				mockStore := &mockProviderStore{
					providers: model.ProviderList{
						{
							ID:           providerID,
							Name:         "stable-provider",
							Endpoint:     server.URL,
							HealthStatus: model.HealthStatusReady,
						},
					},
				}

				instStore := &mockInstanceStore{}
				monitor = healthcheck.NewMonitor(mockStore, instStore, cfg)
				monitor.CheckProviders(ctx)

				Expect(instStore.markProviderDeletionsCalls).To(BeEmpty())
				Expect(instStore.reactivateProviderCalls).To(BeEmpty())
			})

			It("does not call either method when provider stays Unavailable", func() {
				server := failingServer()
				defer server.Close()

				providerID := uuid.New().String()
				mockStore := &mockProviderStore{
					providers: model.ProviderList{
						{
							ID:                  providerID,
							Name:                "still-down-provider",
							Endpoint:            server.URL,
							HealthStatus:        model.HealthStatusUnavailable,
							ConsecutiveFailures: 5,
						},
					},
				}

				instStore := &mockInstanceStore{}
				monitor = healthcheck.NewMonitor(mockStore, instStore, cfg)
				monitor.CheckProviders(ctx)

				Expect(instStore.markProviderDeletionsCalls).To(BeEmpty())
				Expect(instStore.reactivateProviderCalls).To(BeEmpty())
			})

			It("does not call either method when provider stays Unhealthy", func() {
				server := unhealthyServer()
				defer server.Close()

				providerID := uuid.New().String()
				mockStore := &mockProviderStore{
					providers: model.ProviderList{
						{
							ID:           providerID,
							Name:         "still-unhealthy-provider",
							Endpoint:     server.URL,
							HealthStatus: model.HealthStatusUnhealthy,
						},
					},
				}

				instStore := &mockInstanceStore{}
				monitor = healthcheck.NewMonitor(mockStore, instStore, cfg)
				monitor.CheckProviders(ctx)

				Expect(instStore.markProviderDeletionsCalls).To(BeEmpty())
				Expect(instStore.reactivateProviderCalls).To(BeEmpty())
			})
		})
	})
})
