package controller

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"time"

	catalogv1alpha1 "github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	catalogservice "github.com/dcm-project/control-plane/internal/catalog/service"
	"github.com/dcm-project/control-plane/internal/gitops/store"
	"github.com/dcm-project/control-plane/internal/gitops/store/model"
)

// Reconciler handles the reconciliation of a single GitRepository.
type Reconciler struct {
	gitopsStore    store.Store
	catalogSvc     catalogservice.CatalogItemInstanceService
	catalogItemSvc catalogservice.CatalogItemService
	gitClient      GitOperations
}

// NewReconciler creates a new Reconciler.
func NewReconciler(gitopsStore store.Store, catalogSvc catalogservice.CatalogItemInstanceService, catalogItemSvc catalogservice.CatalogItemService, gitClient GitOperations) *Reconciler {
	return &Reconciler{
		gitopsStore:    gitopsStore,
		catalogSvc:     catalogSvc,
		catalogItemSvc: catalogItemSvc,
		gitClient:      gitClient,
	}
}

// gitOperationTimeout is the maximum time allowed for git clone/fetch operations.
const gitOperationTimeout = 2 * time.Minute

// User-value path for labels; avoids overwriting metadata.name.
const gitopsLabelsFieldPath = "metadata.labels"

const (
	gitopsRepositoryLabel = "gitops.dcm.io/repository"
	gitopsCommitLabel     = "gitops.dcm.io/commit"
)

// Reconcile performs a single reconciliation cycle for the given GitRepository.
func (r *Reconciler) Reconcile(ctx context.Context, repo model.GitRepository) error {
	slog.InfoContext(ctx, "Reconciling git repository", "id", repo.ID, "url", repo.URL)

	// 1. Clone or fetch (with timeout to prevent hanging on unresponsive servers)
	gitCtx, gitCancel := context.WithTimeout(ctx, gitOperationTimeout)
	defer gitCancel()

	latestCommit, err := r.gitClient.CloneOrFetch(gitCtx, repo.URL, repo.Branch, repo.ID)
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

	// Abort reconciliation if all files failed to parse — this prevents
	// mass-deletion of existing instances due to transient parse failures.
	if len(parseResult.Instances) == 0 && len(parseResult.Errors) > 0 {
		r.setError(ctx, repo.ID, fmt.Sprintf("all %d YAML files failed to parse, aborting reconciliation", len(parseResult.Errors)))
		return fmt.Errorf("all %d YAML files failed to parse", len(parseResult.Errors))
	}

	// 4. Validate the whole desired state before touching anything. This covers
	// instances that already exist, whose edits produce no create/delete diff and
	// would otherwise be reported as SYNCED without ever reaching the catalog.
	// On failure nothing is applied and last_synced_commit is left untouched, so the
	// last valid managed state is preserved and the bad commit is retried.
	if validationErrors := r.validateDesiredInstances(ctx, repo.ID, latestCommit, parseResult.Instances); len(validationErrors) > 0 {
		r.setError(ctx, repo.ID, fmt.Sprintf("invalid desired state at commit %s: %v", latestCommit, validationErrors))
		return fmt.Errorf("%d invalid desired instances at commit %s", len(validationErrors), latestCommit)
	}

	// 5. Get existing git-managed instance IDs for this repo
	managedIDs, err := r.gitopsStore.ManagedInstance().ListByRepo(ctx, repo.ID)
	if err != nil {
		r.setError(ctx, repo.ID, fmt.Sprintf("failed to list managed instances: %s", err.Error()))
		return fmt.Errorf("list managed instances: %w", err)
	}

	// 6. Classify: create / delete
	desiredByName := make(map[string]DesiredInstance, len(parseResult.Instances))
	for _, d := range parseResult.Instances {
		desiredByName[d.Name] = d
	}

	existingByID := make(map[string]struct{}, len(managedIDs))
	for _, id := range managedIDs {
		existingByID[id] = struct{}{}
	}

	var toCreate []DesiredInstance
	for name, desired := range desiredByName {
		if _, exists := existingByID[name]; !exists {
			toCreate = append(toCreate, desired)
		}
	}

	var toDelete []string // IDs to delete
	for id := range existingByID {
		if _, exists := desiredByName[id]; !exists {
			toDelete = append(toDelete, id)
			slog.InfoContext(ctx, "Will delete instance removed from Git", "id", repo.ID, "instance_id", id)
		}
	}

	if len(toCreate) == 0 && len(toDelete) == 0 {
		slog.InfoContext(ctx, "No lifecycle changes detected", "id", repo.ID)
		r.setSynced(ctx, repo.ID, latestCommit)
		return nil
	}

	// 7. Set status IN_PROGRESS
	_ = r.gitopsStore.GitRepository().UpdateSyncStatus(ctx, repo.ID, "IN_PROGRESS", "Applying lifecycle changes", repo.LastSyncedCommit)

	// 8. Apply creates
	var reconcileErrors []string
	for _, desired := range toCreate {
		slog.InfoContext(ctx, "Creating instance from Git", "id", repo.ID, "instance_name", desired.Name)
		if err := r.createInstance(ctx, repo.ID, latestCommit, desired); err != nil {
			slog.ErrorContext(ctx, "Failed to create instance", "id", repo.ID, "instance_name", desired.Name, "error", err)
			reconcileErrors = append(reconcileErrors, fmt.Sprintf("create %s: %s", desired.Name, err.Error()))
			continue
		}
		if err := r.gitopsStore.ManagedInstance().Add(ctx, repo.ID, desired.Name); err != nil {
			slog.ErrorContext(ctx, "Failed to record managed instance", "id", repo.ID, "instance_name", desired.Name, "error", err)
			reconcileErrors = append(reconcileErrors, fmt.Sprintf("record %s: %s", desired.Name, err.Error()))
		}
	}

	// 9. Apply deletes
	for _, instanceID := range toDelete {
		slog.InfoContext(ctx, "Deleting instance removed from Git", "id", repo.ID, "instance_id", instanceID)
		if err := r.catalogSvc.Delete(ctx, instanceID); err != nil {
			slog.ErrorContext(ctx, "Failed to delete instance", "id", repo.ID, "instance_id", instanceID, "error", err)
			reconcileErrors = append(reconcileErrors, fmt.Sprintf("delete %s: %s", instanceID, err.Error()))
			continue
		}
		if err := r.gitopsStore.ManagedInstance().Remove(ctx, repo.ID, instanceID); err != nil {
			slog.ErrorContext(ctx, "Failed to remove managed instance record", "id", repo.ID, "instance_id", instanceID, "error", err)
			reconcileErrors = append(reconcileErrors, fmt.Sprintf("unrecord %s: %s", instanceID, err.Error()))
		}
	}

	// 10. Update status
	if len(reconcileErrors) > 0 {
		r.setError(ctx, repo.ID, fmt.Sprintf("reconciliation errors: %v", reconcileErrors))
		return fmt.Errorf("reconciliation had %d errors", len(reconcileErrors))
	}

	r.setSynced(ctx, repo.ID, latestCommit)
	slog.InfoContext(ctx, "Reconciliation complete", "id", repo.ID, "commit", latestCommit,
		"created", len(toCreate), "deleted", len(toDelete))
	return nil
}

func (r *Reconciler) createInstance(ctx context.Context, repoID, latestCommit string, desired DesiredInstance) error {
	apiVersion := desired.ApiVersion
	if apiVersion == "" {
		apiVersion = "v1alpha1"
	}

	userValues, err := r.buildUserValues(ctx, repoID, latestCommit, desired)
	if err != nil {
		return err
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

	_, err = r.catalogSvc.Create(ctx, req)
	return err
}

// buildUserValues turns a desired instance from Git into the user values that
// would be submitted to the catalog service, injecting the GitOps labels.
func (r *Reconciler) buildUserValues(ctx context.Context, repoID, latestCommit string, desired DesiredInstance) ([]catalogv1alpha1.UserValue, error) {
	// Look up catalog item to get all resource names
	catalogItem, err := r.catalogItemSvc.Get(ctx, desired.CatalogItemID)
	if err != nil {
		return nil, fmt.Errorf("get catalog item %s: %w", desired.CatalogItemID, err)
	}
	if catalogItem.Spec == nil {
		return nil, fmt.Errorf("catalog item %s has no spec", desired.CatalogItemID)
	}

	// Desired labels first
	desiredAndGitopsLabels := maps.Clone(desired.Labels)
	if desiredAndGitopsLabels == nil {
		desiredAndGitopsLabels = map[string]string{}
	}
	// Append GitOps labels
	desiredAndGitopsLabels[gitopsRepositoryLabel] = repoID
	desiredAndGitopsLabels[gitopsCommitLabel] = latestCommit

	// Inject labels for every resource in the catalog item
	var userValues []catalogv1alpha1.UserValue
	knownResources := make(map[string]bool, len(catalogItem.Spec.Resources))
	for _, resource := range catalogItem.Spec.Resources {
		// One metadata.labels per resource: user_values labels + desiredAndGitopsLabels.
		mergedLabels, err := mergeResourceLabels(resource.Name, desired.UserValues, desiredAndGitopsLabels)
		if err != nil {
			return nil, fmt.Errorf("instance %s: %w", desired.Name, err)
		}
		userValues = append(userValues, catalogv1alpha1.UserValue{
			Resource: resource.Name,
			Path:     gitopsLabelsFieldPath,
			Value:    mergedLabels,
		})
		knownResources[resource.Name] = true
	}

	// Append the user's original values
	for _, uv := range desired.UserValues {
		if uv.Resource == "" {
			return nil, fmt.Errorf("instance %s: %w", desired.Name, catalogservice.ErrUserValueResourceRequired)
		}
		if !knownResources[uv.Resource] {
			return nil, fmt.Errorf("instance %s: %w: %s", desired.Name, catalogservice.ErrUserValueResourceNotFound, uv.Resource)
		}
		if isMetadataLabelsPath(uv.Path) {
			continue
		}
		userValues = append(userValues, catalogv1alpha1.UserValue{
			Resource: uv.Resource,
			Path:     uv.Path,
			Value:    uv.Value,
		})
	}

	return userValues, nil
}

// validateDesired resolves every desired instance against the catalog without
// persisting anything. A manifest the catalog cannot apply must fail the whole
// cycle rather than be reported as synced; see validateDesiredInstances.
func (r *Reconciler) validateDesired(ctx context.Context, repoID, latestCommit string, desired DesiredInstance) error {
	userValues, err := r.buildUserValues(ctx, repoID, latestCommit, desired)
	if err != nil {
		return err
	}
	return r.catalogSvc.ValidateSpec(ctx, catalogv1alpha1.CatalogItemInstanceSpec{
		CatalogItemId: desired.CatalogItemID,
		UserValues:    userValues,
	})
}

// validateDesiredInstances validates every instance in the desired state, including
// ones already managed by this repository. Without this, a semantically invalid edit
// to an existing instance produces an empty create/delete diff and the repository is
// wrongly reported as SYNCED.
func (r *Reconciler) validateDesiredInstances(ctx context.Context, repoID, latestCommit string, desired []DesiredInstance) []string {
	var errs []string
	for _, d := range desired {
		if err := r.validateDesired(ctx, repoID, latestCommit, d); err != nil {
			slog.ErrorContext(ctx, "Invalid desired instance", "id", repoID, "instance_name", d.Name,
				"file", d.SourceFile, "error", err)
			errs = append(errs, fmt.Sprintf("%s (%s): %s", d.Name, d.SourceFile, err.Error()))
		}
	}
	return errs
}

func mergeResourceLabels(resourceName string, userValues []DesiredUserValue, desiredAndGitopsLabels map[string]string) (map[string]string, error) {
	mergedLabels := map[string]string{}
	for _, uv := range userValues {
		if uv.Resource != resourceName || !isMetadataLabelsPath(uv.Path) {
			continue
		}
		labels, err := labelMapFromUserValue(uv.Value)
		if err != nil {
			return nil, fmt.Errorf("resource %s %s: %w", resourceName, gitopsLabelsFieldPath, err)
		}
		maps.Copy(mergedLabels, labels)
	}
	maps.Copy(mergedLabels, desiredAndGitopsLabels)
	return mergedLabels, nil
}

// labelMapFromUserValue coerces YAML-unmarshaled label maps into map[string]string.
// Only used for metadata.labels.
func labelMapFromUserValue(v any) (map[string]string, error) {
	switch m := v.(type) {
	case map[string]string:
		return m, nil
	case map[string]any:
		labels := make(map[string]string, len(m))
		for k, val := range m {
			s, ok := val.(string)
			if !ok {
				return nil, fmt.Errorf("label %q must be a string, got %T", k, val)
			}
			labels[k] = s
		}
		return labels, nil
	default:
		return nil, fmt.Errorf("value must be a map of strings, got %T", v)
	}
}

func isMetadataLabelsPath(path string) bool {
	return path == gitopsLabelsFieldPath
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
