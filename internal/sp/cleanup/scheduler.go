// Package cleanup implements a scheduler for deferred deletions of service type instances.
package cleanup

import (
	"context"
	"sync"
	"time"

	"github.com/dcm-project/control-plane/internal/sp/config"
	"github.com/dcm-project/control-plane/internal/sp/logging"
	rmsvc "github.com/dcm-project/control-plane/internal/sp/service/resource_manager"
	"github.com/dcm-project/control-plane/internal/sp/store"
	"github.com/dcm-project/control-plane/internal/sp/store/model"
)

// Scheduler periodically retries deferred deletions of service type instances.
type Scheduler struct {
	store           store.Store
	instanceService *rmsvc.InstanceService
	interval        time.Duration
	maxRetries      int
	stopCh          chan struct{}
	wg              sync.WaitGroup
}

// NewScheduler creates a new cleanup scheduler.
func NewScheduler(store store.Store, instanceService *rmsvc.InstanceService, cfg *config.CleanupConfig) *Scheduler {
	return &Scheduler{
		store:           store,
		instanceService: instanceService,
		interval:        cfg.Interval,
		maxRetries:      cfg.MaxRetries,
		stopCh:          make(chan struct{}),
	}
}

// Start begins the cleanup scheduling loop.
func (s *Scheduler) Start(ctx context.Context) {
	s.wg.Add(1)
	go s.run(ctx)
}

// Stop gracefully stops the cleanup scheduler.
func (s *Scheduler) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

func (s *Scheduler) run(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.ProcessPendingDeletions(ctx)
		}
	}
}

// ProcessPendingDeletions attempts to complete all deferred deletions.
func (s *Scheduler) ProcessPendingDeletions(ctx context.Context) {
	log := logging.FromContext(ctx)
	pending, err := s.store.ServiceTypeInstance().ListPendingDeletions(ctx)
	if err != nil {
		log.Error("Error listing pending deletions", "error", err)
		return
	}

	for _, instance := range pending {
		select {
		case <-ctx.Done():
			return
		default:
			s.processOne(ctx, instance)
		}
	}
}

func (s *Scheduler) processOne(ctx context.Context, instance model.ServiceTypeInstance) {
	log := logging.FromContext(ctx)

	marked, err := s.store.ServiceTypeInstance().MarkPendingProviderIfNotReady(ctx, instance.ID)
	if err != nil {
		log.Error("Failed to check provider health for instance", "instance_id", instance.ID, "error", err)
		return
	}
	if marked {
		log.Info("Instance marked as PENDING_PROVIDER, provider is not ready", "instance_id", instance.ID, "provider_name", instance.ProviderName)
		return
	}

	if err := s.instanceService.DeleteFromProvider(ctx, &instance); err != nil {
		log.Error("Failed to delete instance from provider", "instance_id", instance.ID, "provider_name", instance.ProviderName, "error", err)
		s.handleRetryOrFail(ctx, instance)
		return
	}
	log.Info("Successfully deleted instance from provider", "instance_id", instance.ID, "provider_name", instance.ProviderName)
}

func (s *Scheduler) handleRetryOrFail(ctx context.Context, instance model.ServiceTypeInstance) {
	log := logging.FromContext(ctx)
	if instance.RetryCount+1 >= s.maxRetries {
		if err := s.store.ServiceTypeInstance().MarkDeletionFailed(ctx, instance.ID); err != nil {
			log.Error("Failed to mark instance as FAILED", "instance_id", instance.ID, "error", err)
		} else {
			log.Warn("Instance exceeded max retries, marked as FAILED", "instance_id", instance.ID, "max_retries", s.maxRetries)
		}
		return
	}

	if err := s.store.ServiceTypeInstance().IncrementDeletionRetry(ctx, instance.ID); err != nil {
		log.Error("Failed to increment retry count for instance", "instance_id", instance.ID, "error", err)
	}
}
