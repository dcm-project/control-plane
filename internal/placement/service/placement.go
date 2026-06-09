// Package service implements the core business logic for resource placement.
package service

import (
	"context"
	"errors"
	"fmt"
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

// CreateResource creates a new placement request.
func (s *PlacementService) CreateResource(ctx context.Context, req *types.Resource, queryId *string) (*types.Resource, error) {
	log := logging.FromContext(ctx)

	// Get or Generate ID
	resourceIDStr := getOrGenerateStringId(queryId)

	// Generate path
	path := fmt.Sprintf("resources/%s", resourceIDStr)

	log.Debug("Creating resource",
		"resource_id", resourceIDStr,
		"catalog_item_instance_id", req.CatalogItemInstanceId,
	)

	// Validate request with policy engine

	// Build policy payload
	policyRequest := policy.EvaluateRequest{
		Spec: req.Spec,
	}

	// Evaluate spec
	log.Debug("Evaluating policy", "resource_id", resourceIDStr)
	policyResponse, err := s.policy.Evaluate(ctx, policyRequest)
	if err != nil {
		log.Error("Policy evaluation failed", "resource_id", resourceIDStr, "error", err)
		return nil, handlePolicyError(err)
	}

	if policyResponse.SelectedProvider == "" {
		log.Error("Policy response missing selected provider",
			"resource_id", resourceIDStr,
			"status", policyResponse.Status,
		)
		return nil, NewPolicyInternalError("policy response missing selected provider")
	}

	// Extract approvalStatus and providerName from policy response
	approvalStatus := policyResponse.Status
	providerName := policyResponse.SelectedProvider

	// Update request with status and selected provider
	req.ApprovalStatus = &approvalStatus
	req.ProviderName = &providerName

	// Convert API resource to store model
	requestModel := resourceToStoreModel(req, resourceIDStr, path)

	// Create resource in store
	created, err := s.store.Resource().Create(ctx, requestModel)
	if err != nil {
		if errors.Is(err, store.ErrResourceIdExist) {
			log.Warn("Duplicate resource ID", "resource_id", resourceIDStr)
			return nil, NewConflictError(fmt.Sprintf("resource with id %s already exists", resourceIDStr))
		}
		log.Error("Failed to create resource in store", "resource_id", resourceIDStr, "error", err)
		return nil, NewInternalError(fmt.Sprintf("failed to create database record for resource %s: %v", resourceIDStr, err))
	}

	log.Debug("Resource persisted in store", "resource_id", resourceIDStr)

	// Send request to SP Resource Manager
	sprmRequest := sprm.CreateResourceRequest{
		ID:           resourceIDStr,
		Spec:         policyResponse.EvaluatedSpec,
		ProviderName: providerName,
	}

	log.Debug("Provisioning resource via SPRM",
		"resource_id", resourceIDStr,
		"catalog_item_instance_id", created.CatalogItemInstanceId,
	)

	sprmResponse, err := s.sprm.CreateResource(ctx, sprmRequest)
	if err != nil {
		// SPRM call failed, rollback the database record
		log.Error("SPRM provisioning failed, rolling back", "resource_id", resourceIDStr, "error", err)
		if delErr := s.rollbackResourceDelete(created.ID); delErr != nil {
			log.Error("Failed to rollback resource after SPRM error",
				"resource_id", created.ID,
				"db_error", delErr,
				"sprm_error", err,
			)
		}
		return nil, handleSPRMError(err)
	}

	log.Info("Resource created successfully",
		"resource_id", created.ID,
		"catalog_item_instance_id", created.CatalogItemInstanceId,
		"provider", providerName,
		"approval_status", approvalStatus,
		"sprm_status", sprmResponse.Status,
	)
	return storeModelToResource(created), nil
}

// DeleteResource removes a placement request by ID.
func (s *PlacementService) DeleteResource(ctx context.Context, requestID string) error {
	log := logging.FromContext(ctx)
	log.Debug("Deleting resource", "resource_id", requestID)

	// First, get the resource to obtain the CatalogItemInstanceId
	resource, err := s.store.Resource().Get(ctx, requestID)
	if err != nil {
		if errors.Is(err, store.ErrResourceNotFound) {
			return NewNotFoundError(fmt.Sprintf("resource %s not found", requestID))
		}
		log.Error("Failed to get resource for deletion", "resource_id", requestID, "error", err)
		return NewInternalError(fmt.Sprintf("failed to retrieve resource for deletion: %v", err))
	}

	// Delete it from the SPRM first before deleting from the database
	log.Debug("Deleting resource from SPRM",
		"resource_id", requestID,
		"catalog_item_instance_id", resource.CatalogItemInstanceId,
	)

	err = s.sprm.DeleteResource(ctx, requestID)
	if err != nil {
		log.Error("SPRM deletion failed, preserving DB record", "resource_id", requestID, "error", err)
		return handleSPRMError(err)
	}

	// Delete record from the database
	err = s.store.Resource().Delete(ctx, requestID)
	if err != nil {
		if errors.Is(err, store.ErrResourceNotFound) {
			return NewNotFoundError(fmt.Sprintf("resource %s not found", requestID))
		}
		log.Error("Failed to delete resource from store", "resource_id", requestID, "error", err)
		return NewInternalError(fmt.Sprintf("failed to delete database record for resource %s: %v", requestID, err))
	}

	log.Info("Resource deleted successfully",
		"resource_id", requestID,
		"catalog_item_instance_id", resource.CatalogItemInstanceId,
	)
	return nil
}

// RehydrateResource re-evaluates an existing resource against current policies
// and creates a new resource with the given newResourceID. The old resource is
// deleted after the new one is successfully provisioned.
func (s *PlacementService) RehydrateResource(ctx context.Context, resourceID, newResourceID string) (*types.Resource, error) {
	log := logging.FromContext(ctx)
	log.Debug("Rehydrating resource",
		"resource_id", resourceID,
		"new_resource_id", newResourceID,
	)

	// Step 1: Retrieve the old resource
	oldResource, err := s.store.Resource().Get(ctx, resourceID)
	if err != nil {
		if errors.Is(err, store.ErrResourceNotFound) {
			return nil, NewNotFoundError(fmt.Sprintf("resource %s not found", resourceID))
		}
		log.Error("Failed to get resource for rehydration", "resource_id", resourceID, "error", err)
		return nil, NewInternalError(fmt.Sprintf("failed to retrieve resource for rehydration: %v", err))
	}

	// Step 2: Re-evaluate the original spec through policy
	policyRequest := policy.EvaluateRequest{
		Spec: oldResource.Spec,
	}

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

	approvalStatus := policyResponse.Status
	providerName := policyResponse.SelectedProvider

	// Step 3: Create new resource in DB
	newPath := fmt.Sprintf("resources/%s", newResourceID)
	newResource := model.Resource{
		ID:                    newResourceID,
		CatalogItemInstanceId: oldResource.CatalogItemInstanceId,
		Spec:                  oldResource.Spec,
		Path:                  newPath,
		ProviderName:          &providerName,
		ApprovalStatus:        &approvalStatus,
	}

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

	log.Debug("Provisioning new resource via SPRM during rehydration",
		"new_resource_id", newResourceID,
		"catalog_item_instance_id", oldResource.CatalogItemInstanceId,
	)

	_, err = s.sprm.CreateResource(ctx, sprmRequest)
	if err != nil {
		// Rollback the new DB record
		log.Error("SPRM provisioning failed during rehydration, rolling back", "new_resource_id", newResourceID, "error", err)
		if delErr := s.rollbackResourceDelete(newResourceID); delErr != nil {
			log.Error("Failed to rollback new resource after SPRM error",
				"new_resource_id", newResourceID,
				"db_error", delErr,
				"sprm_error", err,
			)
		}
		return nil, handleSPRMError(err)
	}

	// Step 5: Delete old resource from SPRM (deferred - non-blocking)
	log.Debug("Deleting old resource from SPRM (deferred)",
		"resource_id", resourceID,
		"catalog_item_instance_id", oldResource.CatalogItemInstanceId,
	)

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

	log.Info("Resource rehydrated successfully",
		"old_resource_id", resourceID,
		"new_resource_id", newResourceID,
		"catalog_item_instance_id", oldResource.CatalogItemInstanceId,
		"provider", providerName,
		"approval_status", approvalStatus,
	)
	return storeModelToResource(created), nil
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
