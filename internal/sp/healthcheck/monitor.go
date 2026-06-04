// Package healthcheck performs periodic health checks on registered service providers.
package healthcheck

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dcm-project/control-plane/internal/sp/config"
	"github.com/dcm-project/control-plane/internal/sp/store/model"
	providerstore "github.com/dcm-project/control-plane/internal/sp/store/provider"
	rmstore "github.com/dcm-project/control-plane/internal/sp/store/resource_manager"
)

type healthCheckResult int

const (
	healthCheckHealthy healthCheckResult = iota
	healthCheckUnhealthy
	healthCheckFailed
)

type healthResponse struct {
	Status string `json:"status"`
}

// Monitor performs periodic health checks on registered service providers
type Monitor struct {
	store                  providerstore.Provider
	instanceStore          rmstore.ServiceTypeInstance
	httpClient             *http.Client
	interval               time.Duration
	stopCh                 chan struct{}
	wg                     sync.WaitGroup
	maxConsecutiveFailures int
	baseBackoffInterval    time.Duration
	maxBackoffInterval     time.Duration
}

// NewMonitor creates a new health check monitor
func NewMonitor(providerStore providerstore.Provider, instanceStore rmstore.ServiceTypeInstance, config *config.HealthCheckConfig) *Monitor {
	return &Monitor{
		store:         providerStore,
		instanceStore: instanceStore,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		interval:               config.Interval,
		stopCh:                 make(chan struct{}),
		maxConsecutiveFailures: config.MaxConsecutiveFailures,
		baseBackoffInterval:    config.BaseBackoffInterval,
		maxBackoffInterval:     config.MaxBackoffInterval,
	}
}

// Start begins the health check monitoring loop
func (m *Monitor) Start(ctx context.Context) {
	m.wg.Add(1)
	go m.run(ctx)
}

// Stop gracefully stops the health check monitor
func (m *Monitor) Stop() {
	close(m.stopCh)
	m.wg.Wait()
}

func (m *Monitor) run(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// Run immediately on start
	m.CheckProviders(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.CheckProviders(ctx)
		}
	}
}

// CheckProviders checks all providers that are due for a health check
func (m *Monitor) CheckProviders(ctx context.Context) {
	now := time.Now()
	providers, err := m.store.ListProvidersForHealthCheck(ctx, now)
	if err != nil {
		slog.Error("Error listing providers for health check", "error", err)
		return
	}

	for _, provider := range providers {
		select {
		case <-ctx.Done():
			return
		default:
			m.checkProvider(ctx, provider)
		}
	}
}

func (m *Monitor) checkProvider(ctx context.Context, provider model.Provider) {
	now := time.Now()
	var newStatus model.HealthStatus
	var consecutiveFailures int

	result := m.performHealthCheck(ctx, provider)
	switch result {
	case healthCheckHealthy:
		newStatus = model.HealthStatusReady
		consecutiveFailures = 0
	case healthCheckUnhealthy:
		newStatus = model.HealthStatusUnhealthy
		consecutiveFailures = 0
	case healthCheckFailed:
		consecutiveFailures = provider.ConsecutiveFailures + 1
		newStatus = provider.HealthStatus
		if consecutiveFailures >= m.maxConsecutiveFailures {
			newStatus = model.HealthStatusUnavailable
		}
	}

	nextCheck := m.CalculateNextCheckTime(now, newStatus, consecutiveFailures)
	if err := m.store.UpdateHealthStatus(ctx, provider.ID, newStatus, consecutiveFailures, nextCheck); err != nil {
		slog.Error("Error updating health status", "provider", provider.Name, "error", err)
		return
	}

	if provider.HealthStatus != newStatus {
		slog.Info("Provider health status changed",
			"provider", provider.Name,
			"old_status", provider.HealthStatus,
			"new_status", newStatus,
		)

		switch newStatus {
		case model.HealthStatusUnhealthy, model.HealthStatusUnavailable:
			if err := m.instanceStore.MarkProviderDeletionsPendingProvider(ctx, provider.Name); err != nil {
				slog.Error("Failed to park deletions for unhealthy provider", "provider", provider.Name, "error", err)
			}
		case model.HealthStatusReady:
			if err := m.instanceStore.ReactivateProviderDeletions(ctx, provider.Name); err != nil {
				slog.Error("Failed to reactivate deletions for recovered provider", "provider", provider.Name, "error", err)
			}
		}
	}
}

func (m *Monitor) performHealthCheck(ctx context.Context, provider model.Provider) healthCheckResult {
	healthURL := strings.TrimRight(provider.Endpoint, "/") + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		slog.Error("Error creating health check request", "provider", provider.Name, "error", err)
		return healthCheckFailed
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		slog.Debug("Health check failed", "provider", provider.Name, "error", err)
		return healthCheckFailed
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Debug("Health check failed", "provider", provider.Name, "status_code", resp.StatusCode)
		return healthCheckFailed
	}

	var hr healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
		slog.Error("Error parsing health check response", "provider", provider.Name, "error", err)
		return healthCheckFailed
	}

	switch hr.Status {
	case "healthy":
		return healthCheckHealthy
	case "unhealthy":
		slog.Debug("Provider reports unhealthy backing provider", "provider", provider.Name)
		return healthCheckUnhealthy
	default:
		slog.Debug("Unknown health status in response", "provider", provider.Name, "status", hr.Status)
		return healthCheckFailed
	}
}

// CalculateNextCheckTime determines when the next health check should occur
// For Ready and Unhealthy providers: standard interval (provider is reachable)
// Exponential backoff for Unavailable providers
// Formula: min(MaxBackoff, BaseInterval * 2^(failures - MaxConsecutiveFailures))
func (m *Monitor) CalculateNextCheckTime(now time.Time, status model.HealthStatus, consecutiveFailures int) time.Time {
	if status != model.HealthStatusUnavailable {
		return now.Add(m.interval)
	}

	exponent := max(consecutiveFailures-m.maxConsecutiveFailures, 0)

	const maxExponent = 10
	exponent = min(exponent, maxExponent)

	backoffMultiplier := math.Pow(2, float64(exponent))
	backoffDuration := min(time.Duration(float64(m.baseBackoffInterval)*backoffMultiplier), m.maxBackoffInterval)

	return now.Add(backoffDuration)
}
