// Package controller implements the GitOps reconciliation loop.
package controller

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/dcm-project/control-plane/internal/gitops/store"
)

// Controller manages per-GitRepository reconciliation goroutines.
type Controller struct {
	reconciler *Reconciler
	store      store.Store
	pollInterval time.Duration // how often to reload repos from DB
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewController creates a new Controller.
// pollInterval controls how often the controller reloads the list of GitRepositories from the DB.
func NewController(reconciler *Reconciler, gitopsStore store.Store, pollInterval time.Duration) *Controller {
	return &Controller{
		reconciler:   reconciler,
		store:        gitopsStore,
		pollInterval: pollInterval,
		stopCh:       make(chan struct{}),
	}
}

// Start begins the main controller loop.
func (c *Controller) Start(ctx context.Context) {
	c.wg.Add(1)
	go c.run(ctx)
}

// Stop gracefully stops the controller and waits for all goroutines to finish.
func (c *Controller) Stop() {
	close(c.stopCh)
	c.wg.Wait()
}

func (c *Controller) run(ctx context.Context) {
	defer c.wg.Done()

	// Run immediately on start, then on ticker
	c.reconcileAll(ctx)

	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.reconcileAll(ctx)
		}
	}
}

func (c *Controller) reconcileAll(ctx context.Context) {
	repos, err := c.store.GitRepository().ListAll(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list git repositories", "error", err)
		return
	}

	for _, repo := range repos {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		default:
		}

		// Check if it's time to reconcile based on interval
		if repo.LastSyncTime != nil {
			nextSync := repo.LastSyncTime.Add(time.Duration(repo.IntervalSeconds) * time.Second)
			if time.Now().Before(nextSync) {
				continue
			}
		}

		if err := c.reconciler.Reconcile(ctx, repo); err != nil {
			slog.ErrorContext(ctx, "Reconciliation failed", "repo_id", repo.ID, "error", err)
		}
	}
}
