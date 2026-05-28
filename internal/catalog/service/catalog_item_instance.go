package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/placement"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/google/uuid"
)

const catalogItemInstanceRollbackTimeout = 10 * time.Second

// CreateCatalogItemInstanceRequest contains the parameters for creating a catalog item instance
type CreateCatalogItemInstanceRequest struct {
	ID          *string                          // Optional user-specified ID
	ApiVersion  string                           // e.g., "v1alpha1"
	DisplayName string                           // Required, max 63 chars
	Spec        v1alpha1.CatalogItemInstanceSpec // Required, contains catalog_item_id and user_values
}

// CatalogItemInstanceListOptions contains options for listing catalog item instances
type CatalogItemInstanceListOptions struct {
	PageToken     *string
	MaxPageSize   *int32
	CatalogItemId *string // Filter by catalog_item_id
}

// CatalogItemInstanceListResult contains the result of a List operation
type CatalogItemInstanceListResult struct {
	CatalogItemInstances []v1alpha1.CatalogItemInstance
	NextPageToken        *string
}

// CatalogItemInstanceService defines the business logic for CatalogItemInstance operations
type CatalogItemInstanceService interface {
	List(ctx context.Context, opts CatalogItemInstanceListOptions) (*CatalogItemInstanceListResult, error)
	Create(ctx context.Context, req *CreateCatalogItemInstanceRequest) (*v1alpha1.CatalogItemInstance, error)
	Get(ctx context.Context, id string) (*v1alpha1.CatalogItemInstance, error)
	Delete(ctx context.Context, id string) error
	Rehydrate(ctx context.Context, id string) (*v1alpha1.CatalogItemInstance, error)
}

type catalogItemInstanceService struct {
	store       store.Store
	specBuilder *specBuilder
	pmClient    placement.Client
	logger      *slog.Logger
}

// newCatalogItemInstanceService creates a new CatalogItemInstanceService instance.
// pmClient must not be nil.
func newCatalogItemInstanceService(store store.Store, pmClient placement.Client, logger *slog.Logger) (CatalogItemInstanceService, error) {
	if pmClient == nil {
		return nil, fmt.Errorf("pmClient must not be nil")
	}
	return &catalogItemInstanceService{
		store:       store,
		specBuilder: newSpecBuilder(store),
		pmClient:    pmClient,
		logger:      logger,
	}, nil
}

// List returns a paginated list of catalog item instances
func (s *catalogItemInstanceService) List(ctx context.Context, opts CatalogItemInstanceListOptions) (*CatalogItemInstanceListResult, error) {
	// Convert service options to store options
	storeOpts := &store.CatalogItemInstanceListOptions{
		PageToken:     opts.PageToken,
		CatalogItemId: opts.CatalogItemId,
	}
	if opts.MaxPageSize != nil {
		storeOpts.PageSize = int(*opts.MaxPageSize)
	}

	// Call store layer
	storeResult, err := s.store.CatalogItemInstance().List(ctx, storeOpts)
	if err != nil {
		return nil, err
	}

	// Convert store models to API types
	apiTypes := make([]v1alpha1.CatalogItemInstance, len(storeResult.CatalogItemInstances))
	for i, storeModel := range storeResult.CatalogItemInstances {
		apiTypes[i] = catalogItemInstanceToAPIType(&storeModel)
	}

	return &CatalogItemInstanceListResult{
		CatalogItemInstances: apiTypes,
		NextPageToken:        storeResult.NextPageToken,
	}, nil
}

// Create creates a new catalog item instance
func (s *catalogItemInstanceService) Create(ctx context.Context, req *CreateCatalogItemInstanceRequest) (*v1alpha1.CatalogItemInstance, error) {
	// Generate IDs
	id := getOrGenerateID(req.ID)
	resourceID := uuid.New().String()
	// Generate path
	path := fmt.Sprintf("catalog-item-instances/%s", id)

	// Build resource spec (resolves reference chain and validates user_values)
	resourceSpec, err := s.specBuilder.BuildResourceSpec(ctx, req.Spec.CatalogItemId, req.Spec.UserValues)
	if err != nil {
		s.logger.WarnContext(ctx, "Failed to build resource spec",
			"id", id,
			"catalog_item_id", req.Spec.CatalogItemId,
			"error", err,
		)
		return nil, err
	}

	// DB first — fail fast on constraint violations (ID conflict, FK violation)
	storeModel := catalogItemInstanceToStoreModel(id, resourceID, path, req)
	createdModel, err := s.store.CatalogItemInstance().Create(ctx, storeModel)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to create catalog item instance in store", "id", id, "error", err)
		return nil, mapCatalogItemInstanceStoreError(err)
	}

	// Call Placement Manager — only after DB validation passes
	s.logger.DebugContext(ctx, "Calling placement manager to create resource", "id", id)
	_, err = s.pmClient.CreateResource(ctx, placement.CreateResourceRequest{
		CatalogItemInstanceID: id,
		Spec:                  resourceSpec,
	}, resourceID)
	if err != nil {
		mapped := mapPlacementError(err, ErrPlacementManagerCreateFailed)
		if rbErr := s.rollbackCatalogItemInstanceCreate(id); rbErr != nil {
			s.logger.ErrorContext(ctx, "Placement manager create failed and rollback delete failed",
				"id", id,
				"placement_error", err,
				"rollback_error", rbErr,
			)
			return nil, fmt.Errorf("%w; additionally, rollback failed: %v", mapped, rbErr)
		}
		s.logger.ErrorContext(ctx, "Placement manager create failed, rolled back DB record",
			"id", id,
			"error", err,
		)
		return nil, mapped
	}

	s.logger.InfoContext(ctx, "Catalog item instance created", "id", id, "catalog_item_id", req.Spec.CatalogItemId)
	// Convert result back to API type
	apiType := catalogItemInstanceToAPIType(createdModel)
	return &apiType, nil
}

// Get retrieves a catalog item instance by ID
func (s *catalogItemInstanceService) Get(ctx context.Context, id string) (*v1alpha1.CatalogItemInstance, error) {
	// Call store layer
	storeModel, err := s.store.CatalogItemInstance().Get(ctx, id)
	if err != nil {
		return nil, mapCatalogItemInstanceStoreError(err)
	}

	// Convert to API type
	apiType := catalogItemInstanceToAPIType(storeModel)
	return &apiType, nil
}

// Rehydrate rehydrates a catalog item instance by generating a new resource ID
// and delegating to the Placement Manager.
// Uses DB-first CAS (compare-and-swap) to prevent concurrent rehydrates: the resource_id is updated
// in the DB before calling PM, so only one concurrent caller can proceed.
func (s *catalogItemInstanceService) Rehydrate(ctx context.Context, id string) (*v1alpha1.CatalogItemInstance, error) {
	// Look up existing instance
	instance, err := s.store.CatalogItemInstance().Get(ctx, id)
	if err != nil {
		return nil, mapCatalogItemInstanceStoreError(err)
	}

	oldResourceID := instance.ResourceID
	// Generate new resource ID
	newResourceID := uuid.New().String()

	// DB first — CAS rejects concurrent callers here
	updatedModel, err := s.store.CatalogItemInstance().UpdateResourceID(ctx, id, oldResourceID, newResourceID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to update resource ID in store",
			"id", id,
			"error", err,
		)
		return nil, mapCatalogItemInstanceStoreError(err)
	}

	// Call Placement Manager rehydrate
	s.logger.DebugContext(ctx, "Calling placement manager to rehydrate resource",
		"id", id,
		"old_resource_id", oldResourceID,
		"new_resource_id", newResourceID,
	)
	_, err = s.pmClient.RehydrateResource(ctx, oldResourceID, newResourceID)
	if err != nil {
		mapped := mapPlacementError(err, ErrPlacementManagerRehydrateFailed)
		if rbErr := s.rollbackRehydrateResourceID(id, newResourceID, oldResourceID); rbErr != nil {
			s.logger.ErrorContext(ctx, "Placement manager rehydrate failed and rollback update failed",
				"id", id,
				"placement_error", err,
				"rollback_error", rbErr,
			)
			return nil, fmt.Errorf("%w; additionally, rollback failed: %v", mapped, rbErr)
		}
		s.logger.ErrorContext(ctx, "Placement manager rehydrate failed, rolled back resource ID",
			"id", id,
			"error", err,
		)
		return nil, mapped
	}

	s.logger.InfoContext(ctx, "Catalog item instance rehydrated",
		"id", id,
		"new_resource_id", newResourceID,
	)

	apiType := catalogItemInstanceToAPIType(updatedModel)
	return &apiType, nil
}

// Delete deletes a catalog item instance by ID
func (s *catalogItemInstanceService) Delete(ctx context.Context, id string) error {
	// Fetch instance for 404 handling and to get the resource ID
	instance, err := s.store.CatalogItemInstance().Get(ctx, id)
	if err != nil {
		return mapCatalogItemInstanceStoreError(err)
	}

	// Delete PM resource using the stored resource ID
	if instance.ResourceID != "" {
		s.logger.DebugContext(ctx, "Calling placement manager to delete resource", "id", id, "resource_id", instance.ResourceID)
		if err := s.pmClient.DeleteResource(ctx, instance.ResourceID); err != nil {
			s.logger.ErrorContext(ctx, "Placement manager delete failed", "id", id, "error", err)
			return fmt.Errorf("%w: %s", ErrPlacementManagerDeleteFailed, err.Error())
		}
	}

	// Delete local record
	err = s.store.CatalogItemInstance().Delete(ctx, id)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to delete catalog item instance from store", "id", id, "error", err)
		return mapCatalogItemInstanceStoreError(err)
	}

	s.logger.InfoContext(ctx, "Catalog item instance deleted", "id", id)
	return nil
}

func (s *catalogItemInstanceService) rollbackCatalogItemInstanceCreate(id string) error {
	rbCtx, cancel := context.WithTimeout(context.Background(), catalogItemInstanceRollbackTimeout)
	defer cancel()
	return s.store.CatalogItemInstance().Delete(rbCtx, id)
}

func (s *catalogItemInstanceService) rollbackRehydrateResourceID(id, newResourceID, oldResourceID string) error {
	rbCtx, cancel := context.WithTimeout(context.Background(), catalogItemInstanceRollbackTimeout)
	defer cancel()
	_, err := s.store.CatalogItemInstance().UpdateResourceID(rbCtx, id, newResourceID, oldResourceID)
	return err
}

// mapPlacementError inspects the error from the placement client and maps
// known HTTP status codes (406, 422, 424) to specific sentinel errors. For
// unrecognised codes or non-PlacementError errors, the genericSentinel is used.
func mapPlacementError(err error, genericSentinel error) error {
	var pmErr *placement.PlacementError
	if errors.As(err, &pmErr) {
		switch pmErr.StatusCode {
		case http.StatusNotAcceptable:
			return fmt.Errorf("%w: %s", ErrPlacementManagerPolicyRejected, pmErr.Error())
		case http.StatusUnprocessableEntity:
			return fmt.Errorf("%w: %s", ErrPlacementManagerProviderError, pmErr.Error())
		case http.StatusFailedDependency:
			return fmt.Errorf("%w: %s", ErrPlacementManagerPolicyDependency, pmErr.Error())
		}
	}
	return fmt.Errorf("%w: %s", genericSentinel, err.Error())
}
