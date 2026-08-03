// Package service implements the core business logic for resource placement.
package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/dcm-project/control-plane/internal/placement/logging"
	"github.com/dcm-project/control-plane/internal/placement/policy"
	"github.com/dcm-project/control-plane/internal/placement/sprm"
	"github.com/dcm-project/control-plane/internal/placement/store"
	"github.com/dcm-project/control-plane/internal/placement/store/model"
	"github.com/dcm-project/control-plane/internal/placement/types"
	"github.com/google/uuid"
)

const resourceRollbackTimeout = 10 * time.Second

// PlacementService handles business logic for placement request management.
type PlacementService struct {
	store  store.Store
	policy policy.Client
	sprm   sprm.Client
}

// NewPlacementService creates a new PlacementService with the given store, policy client, and SPRM client.
func NewPlacementService(store store.Store, policyClient policy.Client, sprmClient sprm.Client) *PlacementService {
	return &PlacementService{
		store:  store,
		policy: policyClient,
		sprm:   sprmClient,
	}
}

// CreateRun executes a placement run for one or more resources.
func (s *PlacementService) CreateRun(ctx context.Context, req *types.CreateRunRequest) (*types.Run, error) {
	log := logging.FromContext(ctx)

	// step 1: validate inputs
	if err := s.validateCreateRunRequest(ctx, req); err != nil {
		return nil, err
	}

	// step 2: assign dag levels
	resourceNameDagLevelMap, err := assignDagLevels(req.Resources)
	if err != nil {
		return nil, err
	}

	runID := req.RunId
	log.Debug("Creating run",
		"run_id", runID,
		"catalog_item_instance_id", req.CatalogItemInstanceId,
		"resource_count", len(req.Resources),
	)

	type preparedResource struct {
		resource      model.Resource
		evaluatedSpec map[string]any
	}

	// step 3: evaluate policy for each resource
	prepared := make([]preparedResource, 0, len(req.Resources))
	for _, resource := range req.Resources {
		// Get or generate ID
		resourceID := getOrGenerateStringId(resource.ID)
		// Generate path
		path := fmt.Sprintf("resources/%s", resourceID)

		// Evaluate spec with policy engine
		policyRequest := policy.EvaluateRequest{Spec: resource.Spec}
		log.Debug("Evaluating policy", "run_id", runID, "resource_id", resourceID, "name", resource.Name)
		policyResponse, err := s.policy.Evaluate(ctx, policyRequest)
		if err != nil {
			log.Error("Policy evaluation failed", "run_id", runID, "resource_id", resourceID, "error", err)
			return nil, handlePolicyError(err)
		}
		if policyResponse.SelectedProvider == "" {
			log.Error("Policy response missing selected provider",
				"run_id", runID,
				"resource_id", resourceID,
				"status", policyResponse.Status,
			)
			return nil, NewPolicyInternalError("policy response missing selected provider")
		}

		// Extract approvalStatus and providerName from policy response
		approval := policyResponse.Status
		provider := policyResponse.SelectedProvider

		prepared = append(prepared, preparedResource{
			resource: model.Resource{
				ID:                    resourceID,
				RunID:                 runID,
				CatalogItemInstanceId: req.CatalogItemInstanceId,
				Name:                  resource.Name,
				Spec:                  resource.Spec,
				RequiresResources:     append([]string(nil), resource.RequiresResources...),
				DagLevel:              resourceNameDagLevelMap[resource.Name],
				Status:                types.ResourceStatusPending,
				Path:                  path,
				ProviderName:          &provider,
				ApprovalStatus:        &approval,
			},
			evaluatedSpec: policyResponse.EvaluatedSpec,
		})
	}

	// step 4: persist resources in store
	rows := make([]model.Resource, 0, len(prepared))
	for _, p := range prepared {
		rows = append(rows, p.resource)
	}
	createdModels, err := s.store.Resource().CreateBatch(ctx, rows)
	if err != nil {
		_ = s.rollbackRunDelete(runID)
		if errors.Is(err, store.ErrResourceIdExist) {
			log.Warn("Duplicate resource ID in create run", "run_id", runID)
			return nil, NewConflictError("one or more resources already exists")
		}
		log.Error("Failed to create resources in store", "run_id", runID, "error", err)
		return nil, NewInternalError(fmt.Sprintf("failed to create database records for run %s: %v", runID, err))
	}

	// step 5: provision dag_level 0 via SPRM (higher levels continue asynchronously later)
	var provisionedIDs []string
	for _, p := range prepared {
		if p.resource.DagLevel != 0 {
			continue
		}
		sprmRequest := sprm.CreateResourceRequest{
			ID:           p.resource.ID,
			Spec:         p.evaluatedSpec,
			ProviderName: *p.resource.ProviderName,
		}
		log.Debug("Provisioning resource via SPRM",
			"run_id", runID,
			"resource_id", p.resource.ID,
			"name", p.resource.Name,
		)
		if _, err := s.sprm.CreateResource(ctx, sprmRequest); err != nil {
			// SPRM call failed, rollback provisioned resources and DB records
			log.Error("SPRM provisioning failed, rolling back create run",
				"run_id", runID,
				"resource_id", p.resource.ID,
				"error", err,
			)
			s.rollbackProvisioned(provisionedIDs)
			if delErr := s.rollbackRunDelete(runID); delErr != nil {
				log.Error("Failed to rollback run after SPRM error",
					"run_id", runID,
					"db_error", delErr,
					"sprm_error", err,
				)
			}
			return nil, handleSPRMError(err)
		}
		provisionedIDs = append(provisionedIDs, p.resource.ID)
	}

	log.Info("Run created successfully",
		"run_id", runID,
		"catalog_item_instance_id", req.CatalogItemInstanceId,
		"resource_count", len(createdModels),
		"level0_provisioned", len(provisionedIDs),
	)
	return storeModelsToRun(createdModels), nil
}

// GetRun returns a run by run_id.
func (s *PlacementService) GetRun(ctx context.Context, runID string) (*types.Run, error) {
	log := logging.FromContext(ctx)
	resources, err := s.store.Resource().ListByRunID(ctx, runID)
	if err != nil {
		log.Error("Failed to get run", "run_id", runID, "error", err)
		return nil, NewInternalError(fmt.Sprintf("failed to retrieve run %s: %v", runID, err))
	}
	if len(resources) == 0 {
		return nil, NewNotFoundError(fmt.Sprintf("run %s not found", runID))
	}
	return storeModelsToRun(resources), nil
}

// ListRun lists runs (resources grouped by run_id).
// TODO: Paginate by distinct run_id (then load full resource sets per run).
// Paginating resource rows first can split a multi-resource run across pages.
func (s *PlacementService) ListRun(ctx context.Context, opts *store.ResourceListOptions) (*types.ListRunResult, error) {
	log := logging.FromContext(ctx)
	result, err := s.store.Resource().List(ctx, opts)
	if err != nil {
		log.Error("Failed to list runs", "error", err)
		return nil, NewInternalError(fmt.Sprintf("failed to list runs: %v", err))
	}

	byRun := make(map[string]model.ResourceList)
	order := make([]string, 0)
	for i := range result.Resources {
		r := result.Resources[i]
		if _, ok := byRun[r.RunID]; !ok {
			order = append(order, r.RunID)
		}
		byRun[r.RunID] = append(byRun[r.RunID], r)
	}

	runs := make([]types.Run, 0, len(order))
	for _, runID := range order {
		run := storeModelsToRun(byRun[runID])
		if run != nil {
			runs = append(runs, *run)
		}
	}

	return &types.ListRunResult{
		Runs:          runs,
		NextPageToken: result.NextPageToken,
	}, nil
}

// DeleteRun starts deletion for a run by run_id.
func (s *PlacementService) DeleteRun(ctx context.Context, runID string) error {
	log := logging.FromContext(ctx)
	log.Debug("Deleting run", "run_id", runID)

	resources, err := s.store.Resource().ListByRunID(ctx, runID)
	if err != nil {
		log.Error("Failed to list resources for delete", "run_id", runID, "error", err)
		return NewInternalError(fmt.Sprintf("failed to retrieve run for deletion: %v", err))
	}
	if len(resources) == 0 {
		return NewNotFoundError(fmt.Sprintf("run %s not found", runID))
	}

	// Find the highest dag_level (first delete wave)
	maxDagLevel := slices.MaxFunc(resources, func(a, b model.Resource) int {
		return cmp.Compare(a.DagLevel, b.DagLevel)
	}).DagLevel

	// Mark all resources in the run as PENDING_DELETION
	if err := s.store.Resource().UpdateStatusByRunID(ctx, runID, types.ResourceStatusPendingDeletion); err != nil {
		log.Error("Failed to mark resources as pending deletion", "run_id", runID, "error", err)
		return NewInternalError(fmt.Sprintf("failed to mark run %s as pending deletion: %v", runID, err))
	}

	for _, resource := range resources {
		if resource.DagLevel != maxDagLevel {
			continue
		}
		// Delete from SPRM first before deleting from the database
		log.Debug("Deleting resource from SPRM",
			"run_id", runID,
			"resource_id", resource.ID,
			"dag_level", resource.DagLevel,
		)
		if err := s.sprm.DeleteResource(ctx, resource.ID); err != nil {
			// No delete-path rollback: already-deleted SPRM resources stay gone.
			log.Error("SPRM deletion failed, preserving remaining DB records",
				"run_id", runID,
				"resource_id", resource.ID,
				"error", err,
			)
			return handleSPRMError(err)
		}
		// Delete record from the database
		if err := s.store.Resource().Delete(ctx, resource.ID); err != nil && !errors.Is(err, store.ErrResourceNotFound) {
			log.Error("Failed to delete resource from store after SPRM success",
				"run_id", runID,
				"resource_id", resource.ID,
				"error", err,
			)
			return NewInternalError(fmt.Sprintf("failed to delete database record for resource %s: %v", resource.ID, err))
		}
	}

	log.Info("Run delete started for highest dag level",
		"run_id", runID,
		"dag_level", maxDagLevel,
	)
	return nil
}

// RehydrateResource re-evaluates an existing resource against current policies
// and creates a new resource under newRunID. The old resource is deleted after
// the new one is successfully provisioned.
// TODO: Rehydrate all resources in the run (e.g. via CreateRun), then delete the
// old run in reverse DAG order (similar to DeleteRun) instead of migrating run_id.
func (s *PlacementService) RehydrateResource(ctx context.Context, runID, newRunID string) (*types.Resource, error) {
	log := logging.FromContext(ctx)
	log.Debug("Rehydrating run",
		"run_id", runID,
		"new_run_id", newRunID,
	)
	if runID == "" {
		return nil, NewValidationError("run_id is required")
	}
	if newRunID == "" {
		return nil, NewValidationError("new_run_id is required")
	}

	resources, err := s.store.Resource().ListByRunID(ctx, runID)
	if err != nil {
		log.Error("Failed to list resources for rehydration", "run_id", runID, "error", err)
		return nil, NewInternalError(fmt.Sprintf("failed to list resources for run %s: %v", runID, err))
	}
	if len(resources) == 0 {
		return nil, NewNotFoundError(fmt.Sprintf("run %s not found", runID))
	}

	// Step 1: Retrieve the old resource (first resource in the run)
	oldResource := resources[0]
	resourceID := oldResource.ID
	// Generate UUID for the replacement resource
	newResourceID := uuid.New().String()

	// Step 2: Re-evaluate the original spec through policy
	policyRequest := policy.EvaluateRequest{Spec: oldResource.Spec}
	log.Debug("Re-evaluating policy for rehydration", "resource_id", resourceID)
	policyResponse, err := s.policy.Evaluate(ctx, policyRequest)
	if err != nil {
		log.Error("Policy re-evaluation failed during rehydration", "resource_id", resourceID, "error", err)
		return nil, handlePolicyError(err)
	}

	if policyResponse.SelectedProvider == "" {
		log.Error("Policy response missing selected provider during rehydration",
			"resource_id", resourceID,
			"status", policyResponse.Status,
		)
		return nil, NewPolicyInternalError("policy response missing selected provider")
	}

	// Extract approvalStatus and providerName from policy response
	approvalStatus := policyResponse.Status
	providerName := policyResponse.SelectedProvider

	// Step 3: Create new resource in DB
	newPath := fmt.Sprintf("resources/%s", newResourceID)
	newResource := model.Resource{
		ID:                    newResourceID,
		RunID:                 newRunID,
		CatalogItemInstanceId: oldResource.CatalogItemInstanceId,
		Name:                  oldResource.Name,
		Spec:                  oldResource.Spec,
		RequiresResources:     append([]string(nil), oldResource.RequiresResources...),
		DagLevel:              oldResource.DagLevel,
		Status:                types.ResourceStatusPending,
		Path:                  newPath,
		ProviderName:          &providerName,
		ApprovalStatus:        &approvalStatus,
	}

	// Step 3: Create new resource in DB
	created, err := s.store.Resource().Create(ctx, newResource)
	if err != nil {
		if errors.Is(err, store.ErrResourceIdExist) {
			log.Warn("Duplicate new resource ID during rehydration", "new_resource_id", newResourceID)
			return nil, NewConflictError(fmt.Sprintf("resource with id %s already exists", newResourceID))
		}
		log.Error("Failed to create new resource during rehydration", "new_resource_id", newResourceID, "error", err)
		return nil, NewInternalError(fmt.Sprintf("failed to create database record for resource %s: %v", newResourceID, err))
	}

	// Step 4: Provision new resource in SPRM
	sprmRequest := sprm.CreateResourceRequest{
		ID:           newResourceID,
		Spec:         policyResponse.EvaluatedSpec,
		ProviderName: providerName,
	}
	if _, err = s.sprm.CreateResource(ctx, sprmRequest); err != nil {
		log.Error("SPRM provisioning failed during rehydration, rolling back", "new_resource_id", newResourceID, "error", err)
		// Rollback the new DB record
		if delErr := s.rollbackResourceDelete(newResourceID); delErr != nil {
			log.Error("Failed to rollback new resource after SPRM error",
				"new_resource_id", newResourceID,
				"db_error", delErr,
				"sprm_error", err,
			)
		}
		return nil, handleSPRMError(err)
	}

	// TODO: Delete the old run in reverse DAG order.

	// Step 5: Delete old resource from SPRM (deferred - non-blocking)
	if err := s.sprm.DeleteResourceDeferred(ctx, resourceID); err != nil {
		log.Error("SPRM deferred deletion failed during rehydration (non-blocking)",
			"resource_id", resourceID,
			"error", err,
		)
	}
	// Step 6: Delete old resource from DB
	if err := s.store.Resource().Delete(ctx, resourceID); err != nil {
		log.Error("Failed to delete old resource from DB during rehydration (non-blocking)",
			"resource_id", resourceID,
			"error", err,
		)
	}

	log.Info("Run rehydrated successfully",
		"old_run_id", runID,
		"new_run_id", newRunID,
		"old_resource_id", resourceID,
		"new_resource_id", newResourceID,
		"catalog_item_instance_id", oldResource.CatalogItemInstanceId,
		"provider", providerName,
		"approval_status", approvalStatus,
	)

	res := storeModelToResource(created)
	return &res, nil
}

func (s *PlacementService) rollbackProvisioned(resourceIDs []string) {
	for _, id := range resourceIDs {
		rbCtx, cancel := context.WithTimeout(context.Background(), resourceRollbackTimeout)
		if err := s.sprm.DeleteResource(rbCtx, id); err != nil {
			logging.FromContext(rbCtx).Error("Failed to tear down provisioned resource during create rollback",
				"resource_id", id,
				"error", err,
			)
		}
		cancel()
	}
}

func (s *PlacementService) rollbackRunDelete(runID string) error {
	rbCtx, cancel := context.WithTimeout(context.Background(), resourceRollbackTimeout)
	defer cancel()
	err := s.store.Resource().DeleteByRunID(rbCtx, runID)
	if errors.Is(err, store.ErrResourceNotFound) {
		return nil
	}
	return err
}

func (s *PlacementService) rollbackResourceDelete(id string) error {
	rbCtx, cancel := context.WithTimeout(context.Background(), resourceRollbackTimeout)
	defer cancel()
	return s.store.Resource().Delete(rbCtx, id)
}

func getOrGenerateStringId(id *string) string {
	if id != nil && *id != "" {
		return *id
	}
	// Generate UUID if not provided
	return uuid.New().String()
}
