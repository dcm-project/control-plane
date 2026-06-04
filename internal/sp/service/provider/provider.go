// Package provider implements business logic for provider management.
package provider

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"time"

	providerserver "github.com/dcm-project/control-plane/internal/sp/api/provider"
	"github.com/dcm-project/control-plane/internal/sp/logging"
	"github.com/dcm-project/control-plane/internal/sp/service"
	"github.com/dcm-project/control-plane/internal/sp/store"
	"github.com/dcm-project/control-plane/internal/sp/store/model"
	providerstore "github.com/dcm-project/control-plane/internal/sp/store/provider"
	"github.com/google/uuid"
)

const (
	defaultPageSize = 100
	maxPageSize     = 100
)

// ListResult contains the result of listing providers with pagination info.
type ListResult struct {
	Providers     []providerserver.Provider
	NextPageToken string
}

// ProviderService handles business logic for provider management.
type ProviderService struct {
	store store.Store
}

// NewProviderService creates a new ProviderService with the given store.
func NewProviderService(store store.Store) *ProviderService {
	return &ProviderService{store: store}
}

// RegisterOrUpdateProvider implements idempotent provider registration per the DCM spec.
// Returns the provider and a boolean indicating whether an existing provider was updated (true) or a new one was created (false).
// Returns service.ErrCodeConflict if name exists with different ID or ID exists with different name.
func (s *ProviderService) RegisterOrUpdateProvider(ctx context.Context, req *providerserver.Provider, queryID *string) (*providerserver.Provider, bool, error) {
	log := logging.FromContext(ctx)
	log.Debug("RegisterOrUpdateProvider request received", "name", req.Name, "client_id", queryID)

	requestedID := s.parseProviderID(req.Id, queryID)

	existing, err := s.findExistingByName(ctx, req.Name, requestedID)
	if err != nil {
		return nil, false, err
	}

	if existing != nil {
		log.Debug("Existing provider found, updating", "provider_id", existing.ID, "name", req.Name)
		existing.ConsecutiveFailures = 0
		existing.NextHealthCheck = nil
		updated, err := s.updateExistingProvider(ctx, existing, req)
		if err != nil {
			return nil, false, err
		}
		return ModelToProvider(updated), true, nil
	}

	providerID, err := s.resolveProviderID(ctx, requestedID)
	if err != nil {
		return nil, false, err
	}

	providerModel := ProviderToModel(req, *providerID)
	created, err := s.store.Provider().Create(ctx, providerModel)
	if err != nil {
		log.Error("Failed to create provider in store", "name", req.Name, "error", err)
		return nil, false, err
	}

	log.Info("Provider created", "provider_id", created.ID, "name", created.Name)
	return ModelToProvider(created), false, nil
}

// parseProviderID extracts the provider ID from request body or query parameter.
func (s *ProviderService) parseProviderID(bodyID *string, queryID *string) *string {
	if bodyID != nil {
		id := bodyID
		return id
	}
	if queryID != nil {
		id := queryID
		return id
	}
	return nil
}

// findExistingByName returns the existing provider if name exists and is valid for update.
// Returns service.ErrCodeConflict if name exists with a different ID than requested.
func (s *ProviderService) findExistingByName(ctx context.Context, name string, requestedID *string) (*model.Provider, error) {
	log := logging.FromContext(ctx)

	existing, err := s.store.Provider().GetByName(ctx, name)
	if err != nil {
		if errors.Is(err, providerstore.ErrProviderNotFound) {
			return nil, nil
		}
		log.Error("Failed to look up provider by name", "name", name, "error", err)
		return nil, err
	}

	if requestedID != nil && existing.ID != *requestedID {
		log.Warn("Name conflict detected", "name", name, "existing_id", existing.ID, "requested_id", *requestedID)
		return nil, &service.ServiceError{
			Code:    service.ErrCodeConflict,
			Message: fmt.Sprintf("name '%s' already exists with a different provider ID", name),
		}
	}

	return existing, nil
}

// resolveProviderID returns the requested ID after checking for conflicts, or generates a new one.
func (s *ProviderService) resolveProviderID(ctx context.Context, requestedID *string) (*string, error) {
	log := logging.FromContext(ctx)

	if requestedID == nil {
		generatedID := uuid.New().String()
		log.Debug("Generated provider ID", "provider_id", generatedID)
		return &generatedID, nil
	}

	exists, err := s.store.Provider().ExistsByID(ctx, *requestedID)
	if err != nil {
		log.Error("Failed to check provider ID existence", "provider_id", *requestedID, "error", err)
		return nil, err
	}
	if exists {
		log.Warn("Duplicate provider ID", "provider_id", *requestedID)
		return nil, &service.ServiceError{
			Code:    service.ErrCodeConflict,
			Message: fmt.Sprintf("provider with ID '%s' already exists", *requestedID),
		}
	}

	return requestedID, nil
}

func (s *ProviderService) updateExistingProvider(ctx context.Context, existing *model.Provider, req *providerserver.Provider) (*model.Provider, error) {
	log := logging.FromContext(ctx)

	applyProviderRequestToModel(existing, req)
	existing.UpdateTime = time.Now()

	updated, err := s.store.Provider().Update(ctx, *existing)
	if err != nil {
		log.Error("Failed to update provider in store", "provider_id", existing.ID, "error", err)
		return nil, err
	}

	log.Info("Provider updated", "provider_id", updated.ID, "name", updated.Name)
	return updated, nil
}

// GetProvider retrieves a provider by ID. Returns service.ErrCodeNotFound if not found.
func (s *ProviderService) GetProvider(ctx context.Context, providerID string) (*providerserver.Provider, error) {
	log := logging.FromContext(ctx)
	log.Debug("Getting provider", "provider_id", providerID)

	provider, err := s.store.Provider().Get(ctx, providerID)
	if err != nil {
		if errors.Is(err, providerstore.ErrProviderNotFound) {
			return nil, &service.ServiceError{Code: service.ErrCodeNotFound, Message: fmt.Sprintf("provider %s not found", providerID)}
		}
		log.Error("Failed to get provider from store", "provider_id", providerID, "error", err)
		return nil, err
	}

	return ModelToProvider(provider), nil
}

// ListProviders returns providers with pagination support per AEP-158.
func (s *ProviderService) ListProviders(ctx context.Context, serviceType string, requestedPageSize int, pageToken string) (*ListResult, error) {
	log := logging.FromContext(ctx)
	log.Debug("Listing providers",
		"service_type", serviceType,
		"page_size", requestedPageSize,
	)
	// Validate and normalize page size per AEP-158
	pageSize := requestedPageSize
	if pageSize < 0 {
		return nil, &service.ServiceError{Code: service.ErrCodeValidation, Message: "max_page_size must not be negative"}
	}
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	// Decode page token to get offset
	offset := 0
	if pageToken != "" {
		decoded, err := DecodePageToken(pageToken)
		if err != nil {
			return nil, &service.ServiceError{Code: service.ErrCodeValidation, Message: "invalid page_token"}
		}
		offset = decoded
	}

	// Build filter
	var filter *providerstore.ProviderFilter
	if serviceType != "" {
		filter = &providerstore.ProviderFilter{ServiceType: &serviceType}
	}

	// Get total count for next page calculation
	total, err := s.store.Provider().Count(ctx, filter)
	if err != nil {
		log.Error("Failed to count providers", "error", err)
		return nil, err
	}

	// Fetch providers with pagination
	pagination := &providerstore.Pagination{Limit: pageSize, Offset: offset}
	providers, err := s.store.Provider().List(ctx, filter, pagination)
	if err != nil {
		log.Error("Failed to list providers from store", "error", err)
		return nil, err
	}

	// Convert to API types
	result := make([]providerserver.Provider, len(providers))
	for i, p := range providers {
		result[i] = *ModelToProvider(&p)
	}

	// Calculate next page token
	var nextPageToken string
	nextOffset := offset + len(providers)
	if int64(nextOffset) < total {
		nextPageToken = encodePageToken(nextOffset)
	}

	log.Debug("Providers listed",
		"count", len(result),
		"has_next_page", nextPageToken != "",
	)
	return &ListResult{
		Providers:     result,
		NextPageToken: nextPageToken,
	}, nil
}

func encodePageToken(offset int) string {
	return base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func DecodePageToken(token string) (int, error) {
	decoded, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(decoded))
}

// UpdateProvider updates an existing provider. Returns service.ErrCodeNotFound if provider
// doesn't exist, or service.ErrCodeConflict if the new name is already taken.
func (s *ProviderService) UpdateProvider(ctx context.Context, providerID string, update *providerserver.Provider) (*providerserver.Provider, error) {
	log := logging.FromContext(ctx)
	log.Debug("Updating provider", "provider_id", providerID)

	existing, err := s.store.Provider().Get(ctx, providerID)
	if err != nil {
		if errors.Is(err, providerstore.ErrProviderNotFound) {
			return nil, &service.ServiceError{Code: service.ErrCodeNotFound, Message: fmt.Sprintf("provider %s not found", providerID)}
		}
		log.Error("Failed to get provider for update", "provider_id", providerID, "error", err)
		return nil, err
	}

	// Check for name conflict
	if update.Name != existing.Name {
		other, err := s.store.Provider().GetByName(ctx, update.Name)
		if err != nil && !errors.Is(err, providerstore.ErrProviderNotFound) {
			log.Error("Failed to check name conflict", "provider_id", providerID, "name", update.Name, "error", err)
			return nil, err
		}
		if other != nil && other.ID != providerID {
			log.Warn("Name conflict during update", "provider_id", providerID, "name", update.Name)
			return nil, &service.ServiceError{Code: service.ErrCodeConflict, Message: fmt.Sprintf("name '%s' is already taken", update.Name)}
		}
	}

	updated, err := s.updateExistingProvider(ctx, existing, update)
	if err != nil {
		return nil, err
	}

	return ModelToProvider(updated), nil
}

// DeleteProvider removes a provider by ID. Returns service.ErrCodeNotFound if not found.
func (s *ProviderService) DeleteProvider(ctx context.Context, providerID string) error {
	log := logging.FromContext(ctx)
	log.Debug("Deleting provider", "provider_id", providerID)

	err := s.store.Provider().Delete(ctx, providerID)
	if err != nil {
		if errors.Is(err, providerstore.ErrProviderNotFound) {
			return &service.ServiceError{Code: service.ErrCodeNotFound, Message: fmt.Sprintf("provider %s not found", providerID)}
		}
		log.Error("Failed to delete provider from store", "provider_id", providerID, "error", err)
		return err
	}
	log.Info("Provider deleted", "provider_id", providerID)
	return nil
}
