package controller

import (
	"context"
	"fmt"
	"log/slog"

	catalogv1alpha1 "github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	catalogservice "github.com/dcm-project/control-plane/internal/catalog/service"
	"github.com/dcm-project/control-plane/internal/gitops/store"
	"github.com/dcm-project/control-plane/internal/gitops/store/model"
)

const (
	labelManagedBy  = "gitops.dcm.io/managed-by"
	labelRepository = "gitops.dcm.io/repository"
	labelSourcePath = "gitops.dcm.io/source-path"
	labelCommit     = "gitops.dcm.io/commit"
)

// Reconciler handles the reconciliation of a single GitRepository.
type Reconciler struct {
	gitopsStore store.Store
	catalogSvc  catalogservice.CatalogItemInstanceService
	gitClient   GitOperations
}

// NewReconciler creates a new Reconciler.
func NewReconciler(gitopsStore store.Store, catalogSvc catalogservice.CatalogItemInstanceService, gitClient GitOperations) *Reconciler {
	return &Reconciler{
		gitopsStore: gitopsStore,
		catalogSvc:  catalogSvc,
		gitClient:   gitClient,
	}
}

// Reconcile performs a single reconciliation cycle for the given GitRepository.
func (r *Reconciler) Reconcile(ctx context.Context, repo model.GitRepository) error {
	slog.InfoContext(ctx, "Reconciling git repository", "id", repo.ID, "url", repo.URL)

	// 1. Clone or fetch
	latestCommit, err := r.gitClient.CloneOrFetch(ctx, repo.URL, repo.Branch, repo.ID)
	if err != nil {
		r.setError(ctx, repo.ID, fmt.Sprintf("git fetch failed: %s", err.Error()))
		return fmt.Errorf("git fetch: %w", err)
	}

	// 2. Check if commit changed
	if latestCommit == repo.LastSyncedCommit {
		slog.DebugContext(ctx, "No new commits", "id", repo.ID, "commit", latestCommit)
		r.setSynced(ctx, repo.ID, latestCommit)
		return nil
	}

	// 3. Parse YAML files at spec.path
	workDir := r.gitClient.WorkDir(repo.ID)
	parseResult := ParseCatalogItemInstances(workDir, repo.Path)

	for _, pe := range parseResult.Errors {
		slog.WarnContext(ctx, "Parse error", "id", repo.ID, "file", pe.File, "error", pe.Err)
	}

	// 4. Get existing git-managed instances for this repo
	existingInstances, err := r.listManagedInstances(ctx, repo.ID)
	if err != nil {
		r.setError(ctx, repo.ID, fmt.Sprintf("failed to list managed instances: %s", err.Error()))
		return fmt.Errorf("list managed instances: %w", err)
	}

	// 5. Classify: create / delete
	desiredByName := make(map[string]DesiredInstance, len(parseResult.Instances))
	for _, d := range parseResult.Instances {
		desiredByName[d.Name] = d
	}

	existingByName := make(map[string]string, len(existingInstances)) // name -> id
	for _, inst := range existingInstances {
		existingByName[inst.DisplayName] = *inst.Uid
	}

	var toCreate []DesiredInstance
	for name, desired := range desiredByName {
		if _, exists := existingByName[name]; !exists {
			toCreate = append(toCreate, desired)
		}
	}

	var toDelete []string // IDs to delete
	for name, id := range existingByName {
		if _, exists := desiredByName[name]; !exists {
			toDelete = append(toDelete, id)
			slog.InfoContext(ctx, "Will delete instance removed from Git", "id", repo.ID, "instance_name", name, "instance_id", id)
		}
	}

	if len(toCreate) == 0 && len(toDelete) == 0 {
		slog.InfoContext(ctx, "No lifecycle changes detected", "id", repo.ID)
		r.setSynced(ctx, repo.ID, latestCommit)
		return nil
	}

	// 6. Set status IN_PROGRESS
	_ = r.gitopsStore.GitRepository().UpdateSyncStatus(ctx, repo.ID, "IN_PROGRESS", "Applying lifecycle changes", repo.LastSyncedCommit)

	// 7. Apply creates
	var reconcileErrors []string
	for _, desired := range toCreate {
		slog.InfoContext(ctx, "Creating instance from Git", "id", repo.ID, "instance_name", desired.Name)
		if err := r.createInstance(ctx, repo.ID, latestCommit, desired); err != nil {
			slog.ErrorContext(ctx, "Failed to create instance", "id", repo.ID, "instance_name", desired.Name, "error", err)
			reconcileErrors = append(reconcileErrors, fmt.Sprintf("create %s: %s", desired.Name, err.Error()))
		}
	}

	// 8. Apply deletes
	for _, instanceID := range toDelete {
		slog.InfoContext(ctx, "Deleting instance removed from Git", "id", repo.ID, "instance_id", instanceID)
		if err := r.catalogSvc.Delete(ctx, instanceID); err != nil {
			slog.ErrorContext(ctx, "Failed to delete instance", "id", repo.ID, "instance_id", instanceID, "error", err)
			reconcileErrors = append(reconcileErrors, fmt.Sprintf("delete %s: %s", instanceID, err.Error()))
		}
	}

	// 9. Update status
	if len(reconcileErrors) > 0 {
		r.setError(ctx, repo.ID, fmt.Sprintf("reconciliation errors: %v", reconcileErrors))
		return fmt.Errorf("reconciliation had %d errors", len(reconcileErrors))
	}

	r.setSynced(ctx, repo.ID, latestCommit)
	slog.InfoContext(ctx, "Reconciliation complete", "id", repo.ID, "commit", latestCommit,
		"created", len(toCreate), "deleted", len(toDelete))
	return nil
}

func (r *Reconciler) createInstance(ctx context.Context, repoID, commit string, desired DesiredInstance) error {
	apiVersion := desired.ApiVersion
	if apiVersion == "" {
		apiVersion = "v1alpha1"
	}

	userValues := make([]catalogv1alpha1.UserValue, len(desired.UserValues))
	for i, uv := range desired.UserValues {
		userValues[i] = catalogv1alpha1.UserValue{
			Resource: uv.Resource,
			Path:     uv.Path,
			Value:    uv.Value,
		}
	}

	req := &catalogservice.CreateCatalogItemInstanceRequest{
		ApiVersion:  apiVersion,
		DisplayName: desired.DisplayName,
		Spec: catalogv1alpha1.CatalogItemInstanceSpec{
			CatalogItemId: desired.CatalogItemID,
			UserValues:    userValues,
		},
	}
	// Use the desired instance name as the ID
	req.ID = &desired.Name

	_, err := r.catalogSvc.Create(ctx, req)
	return err
}

func (r *Reconciler) listManagedInstances(ctx context.Context, repoID string) ([]catalogv1alpha1.CatalogItemInstance, error) {
	// List all instances and filter by gitops labels.
	// A future optimization can add label-based filtering to the store.
	result, err := r.catalogSvc.List(ctx, catalogservice.CatalogItemInstanceListOptions{})
	if err != nil {
		return nil, err
	}

	// TODO: filter by gitops labels once CatalogItemInstance supports labels.
	// For now, return all instances — the reconciler will match by name.
	return result.CatalogItemInstances, nil
}

func (r *Reconciler) setSynced(ctx context.Context, repoID, commit string) {
	if err := r.gitopsStore.GitRepository().UpdateSyncStatus(ctx, repoID, "SYNCED", "", commit); err != nil {
		slog.ErrorContext(ctx, "Failed to update sync status to SYNCED", "id", repoID, "error", err)
	}
}

func (r *Reconciler) setError(ctx context.Context, repoID, message string) {
	if err := r.gitopsStore.GitRepository().UpdateSyncStatus(ctx, repoID, "ERROR", message, ""); err != nil {
		slog.ErrorContext(ctx, "Failed to update sync status to ERROR", "id", repoID, "error", err)
	}
}
