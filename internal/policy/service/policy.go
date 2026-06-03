// Package service implements the business logic for policy operations.
package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dcm-project/control-plane/api/policy/v1alpha1"
	"github.com/dcm-project/control-plane/internal/policy/logging"
	"github.com/dcm-project/control-plane/internal/policy/opa"
	"github.com/dcm-project/control-plane/internal/policy/store"
	"github.com/dcm-project/control-plane/internal/policy/store/model"
	"github.com/google/uuid"
)

const (
	MinPriority           = 1
	MaxPriority           = 1000
	policyRollbackTimeout = 10 * time.Second
)

// AEP-122 compliant ID format: 1-63 chars, start with lowercase letter,
// contain only lowercase letters, numbers, and hyphens, end with letter or number
var idPattern = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)

// PolicyService defines the interface for policy business logic operations.
type PolicyService interface {
	CompileAll(ctx context.Context) error
	CreatePolicy(ctx context.Context, policy v1alpha1.Policy, clientID *string) (*v1alpha1.Policy, error)
	GetPolicy(ctx context.Context, id string) (*v1alpha1.Policy, error)
	ListPolicies(ctx context.Context, filter *string, orderBy *string, pageToken *string, pageSize *int32) (*v1alpha1.PolicyList, error)
	UpdatePolicy(ctx context.Context, id string, patch *v1alpha1.Policy) (*v1alpha1.Policy, error)
	DeletePolicy(ctx context.Context, id string) error
}

// PolicyServiceImpl implements the PolicyService interface.
type PolicyServiceImpl struct {
	store  store.Store
	engine opa.Engine
}

var _ PolicyService = (*PolicyServiceImpl)(nil)

// NewPolicyService creates a new PolicyService instance.
func NewPolicyService(store store.Store, engine opa.Engine) *PolicyServiceImpl {
	return &PolicyServiceImpl{
		store:  store,
		engine: engine,
	}
}

func validatePostInput(policy v1alpha1.Policy) error {
	if policy.DisplayName == nil || strings.TrimSpace(*policy.DisplayName) == "" {
		return NewInvalidArgumentError(
			"display_name is required",
			"The display_name field must be present and non-empty",
		)
	}

	if policy.PolicyType == nil {
		return NewInvalidArgumentError(
			"policy_type is required",
			"The policy_type field must be present (GLOBAL or USER)",
		)
	}

	if policy.RegoCode == nil || strings.TrimSpace(*policy.RegoCode) == "" {
		return NewInvalidArgumentError(
			"rego_code is required",
			"The rego_code field must be present and non-empty",
		)
	}

	if err := validatePriority(policy.Priority); err != nil {
		return err
	}

	return nil
}

func getPolicyID(clientID *string) (*string, error) {
	var policyID string

	if clientID != nil && *clientID != "" {
		policyID = *clientID
		// Validate ID format (AEP-122 compliant) only for client-specified IDs
		if !idPattern.MatchString(policyID) {
			return nil, NewInvalidArgumentError(
				"Invalid policy ID format",
				fmt.Sprintf("Policy ID '%s' does not match required format: 1-63 characters, start with lowercase letter, contain only lowercase letters, numbers, and hyphens, end with letter or number", policyID),
			)
		}
	} else {
		// Generate UUID for server-assigned ID
		policyID = uuid.New().String()
	}
	return &policyID, nil
}

func validatePriority(priority *int32) error {
	if priority != nil && (*priority < MinPriority || *priority > MaxPriority) {
		return NewInvalidArgumentError(
			"priority must be between 1 and 1000",
			"The priority field must be a value between 1 and 1000",
		)
	}
	return nil
}

// CompileAll loads all policies from the store and compiles them into the engine.
func (s *PolicyServiceImpl) CompileAll(ctx context.Context) error {
	return s.recompileEngine(ctx)
}

// recompileEngine loads all policies from the store and recompiles the engine.
func (s *PolicyServiceImpl) recompileEngine(ctx context.Context) error {
	allPolicies, err := s.store.Policy().ListAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to list policies for recompilation: %w", err)
	}

	modules := make([]opa.PolicyModule, len(allPolicies))
	for i, p := range allPolicies {
		modules[i] = opa.PolicyModule{
			ID:       p.ID,
			RegoCode: p.RegoCode,
		}
	}

	return s.engine.Compile(ctx, modules)
}

// CreatePolicy creates a new policy resource.
// Required fields (display_name, policy_type, rego_code) are enforced here since the schema has no required.
func (s *PolicyServiceImpl) CreatePolicy(ctx context.Context, policy v1alpha1.Policy, clientID *string) (*v1alpha1.Policy, error) {
	if err := validatePostInput(policy); err != nil {
		return nil, err
	}

	policyID, err := getPolicyID(clientID)
	if err != nil {
		return nil, err
	}

	log := logging.FromContext(ctx)
	log.Debug("Creating policy", "policy_id", *policyID)

	// Validate Rego via engine
	if err := s.engine.ValidateRego(ctx, *policy.RegoCode); err != nil {
		return nil, handleEngineError(err, "create")
	}

	// Convert API model to DB model (includes RegoCode)
	dbPolicy := APIToDBModel(policy, *policyID)

	// Create policy in store (duplicate ID fails here)
	created, err := s.store.Policy().Create(ctx, dbPolicy)
	if err != nil {
		log.Error("Failed to create policy in store", "policy_id", *policyID, "error", err)
		return nil, processPolicyStoreError(err, dbPolicy, "create")
	}

	// Recompile the engine with the new policy
	if err := s.recompileEngine(ctx); err != nil {
		log.Error("Failed to recompile engine after create, rolling back DB", "policy_id", *policyID, "error", err)
		// Rollback: Delete from DB since recompilation failed
		if delErr := s.rollbackPolicyDelete(*policyID); delErr != nil {
			log.Error("Failed to rollback DB policy after compile failure",
				"policy_id", *policyID,
				"db_error", delErr,
				"compile_error", err)
		}
		return nil, NewInternalError("Failed to compile policies after create", err.Error(), err)
	}

	// Convert back to API model
	apiPolicy := DBToAPIModel(created)

	log.Debug("Policy created successfully", "policy_id", *policyID)
	return &apiPolicy, nil
}

// GetPolicy retrieves a policy by ID.
func (s *PolicyServiceImpl) GetPolicy(ctx context.Context, id string) (*v1alpha1.Policy, error) {
	log := logging.FromContext(ctx)
	log.Debug("Getting policy", "policy_id", id)

	// Get policy from store (includes RegoCode)
	dbPolicy, err := s.store.Policy().Get(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrPolicyNotFound) {
			return nil, NewPolicyNotFoundError(id)
		}
		log.Error("Failed to get policy from store", "policy_id", id, "error", err)
		return nil, NewInternalError("Failed to get policy", err.Error(), err)
	}

	// Convert to API model (includes RegoCode from DB)
	apiPolicy := DBToAPIModel(dbPolicy)

	return &apiPolicy, nil
}

func getListOptions(filter *string, orderBy *string, pageToken *string, pageSize *int32) (*store.PolicyListOptions, error) {
	// Parse filter expression
	var policyFilter *store.PolicyFilter
	var err error
	if filter != nil && *filter != "" {
		policyFilter, err = parseFilter(*filter)
		if err != nil {
			return nil, err // Already a ServiceError
		}
	}

	// Parse order by parameter (ParseOrderBy handles nil/empty with default)
	orderByStr := ""
	if orderBy != nil {
		orderByStr = *orderBy
	}
	orderByStr, err = parseOrderBy(orderByStr)
	if err != nil {
		return nil, err // Already a ServiceError
	}

	// Validate and set page size (default: 50, max: 1000)
	pageSizeInt := 50
	if pageSize != nil {
		if *pageSize < 1 {
			return nil, NewInvalidArgumentError(
				"Invalid page size",
				"Page size must be at least 1",
			)
		}
		if *pageSize > 1000 {
			return nil, NewInvalidArgumentError(
				"Invalid page size",
				"Page size must not exceed 1000",
			)
		}
		pageSizeInt = int(*pageSize)
	}

	// Build list options
	return &store.PolicyListOptions{
		Filter:    policyFilter,
		OrderBy:   orderByStr,
		PageToken: pageToken,
		PageSize:  pageSizeInt,
	}, nil
}

// ListPolicies lists policies with optional filtering, ordering, and pagination.
func (s *PolicyServiceImpl) ListPolicies(ctx context.Context, filter *string, orderBy *string, pageToken *string, pageSize *int32) (*v1alpha1.PolicyList, error) {
	log := logging.FromContext(ctx)
	log.Debug("Listing policies")

	opts, err := getListOptions(filter, orderBy, pageToken, pageSize)
	if err != nil {
		return nil, err
	}

	// List policies from store
	result, err := s.store.Policy().List(ctx, opts)
	if err != nil {
		if errors.Is(err, store.ErrInvalidPageToken) {
			return nil, err
		}
		log.Error("Failed to list policies from store", "error", err)
		return nil, NewInternalError("Failed to list policies", err.Error(), err)
	}

	// Convert all DB models to API models
	apiPolicies := make([]v1alpha1.Policy, len(result.Policies))
	for i, dbPolicy := range result.Policies {
		apiPolicies[i] = DBToAPIModel(&dbPolicy)
	}

	// Build response
	response := &v1alpha1.PolicyList{
		Policies: apiPolicies,
	}

	if result.NextPageToken != "" {
		response.NextPageToken = &result.NextPageToken
	}

	log.Debug("Policies listed", "count", len(apiPolicies), "has_next_page", result.NextPageToken != "")
	return response, nil
}

// mergePolicyOntoPolicy merges a PATCH body (Policy) onto an existing policy per RFC 7396.
// Only non-nil mutable fields in patch are applied. Read-only and immutable fields (path, id, policy_type, create_time, update_time) are ignored.
func mergePolicyOntoPolicy(patch *v1alpha1.Policy, existing v1alpha1.Policy) v1alpha1.Policy {
	merged := existing
	if patch == nil {
		return merged
	}
	if patch.DisplayName != nil {
		merged.DisplayName = patch.DisplayName
	}
	if patch.Description != nil {
		merged.Description = patch.Description
	}
	if patch.Enabled != nil {
		merged.Enabled = patch.Enabled
	}
	if patch.LabelSelector != nil {
		merged.LabelSelector = patch.LabelSelector
	}
	if patch.Priority != nil {
		merged.Priority = patch.Priority
	}
	if patch.RegoCode != nil {
		merged.RegoCode = patch.RegoCode
	}
	// policy_type, path, id, create_time, update_time are immutable/read-only; do not merge
	return merged
}

func validatePatchInput(patch *v1alpha1.Policy) error {
	if patch == nil {
		return nil
	}
	if patch.RegoCode != nil && strings.TrimSpace(*patch.RegoCode) == "" {
		return NewInvalidArgumentError(
			"rego_code cannot be empty",
			"When rego_code is provided in the patch it must be non-empty",
		)
	}
	if err := validatePriority(patch.Priority); err != nil {
		return err
	}

	return nil
}

// validatePatchImmutableFields returns an error if the patch attempts to change any readOnly or immutable field.
// Fields present in patch (non-nil) must match the existing policy; omitting a field (nil) is allowed.
func validatePatchImmutableFields(patch *v1alpha1.Policy, existing v1alpha1.Policy) error {
	if patch == nil {
		return nil
	}
	if patch.Path != nil {
		if existing.Path == nil || *patch.Path != *existing.Path {
			return NewInvalidArgumentError(
				"path cannot be updated",
				"The path field is read-only and cannot be changed",
			)
		}
	}
	if patch.Id != nil {
		if existing.Id == nil || *patch.Id != *existing.Id {
			return NewInvalidArgumentError(
				"id cannot be updated",
				"The id field is read-only and cannot be changed",
			)
		}
	}
	if patch.PolicyType != nil {
		if existing.PolicyType == nil || *patch.PolicyType != *existing.PolicyType {
			return NewInvalidArgumentError(
				"policy_type is immutable",
				"The policy_type field cannot be changed after creation",
			)
		}
	}
	if patch.CreateTime != nil {
		if existing.CreateTime == nil || !patch.CreateTime.Equal(*existing.CreateTime) {
			return NewInvalidArgumentError(
				"create_time cannot be updated",
				"The create_time field is read-only and cannot be changed",
			)
		}
	}
	if patch.UpdateTime != nil {
		if existing.UpdateTime == nil || !patch.UpdateTime.Equal(*existing.UpdateTime) {
			return NewInvalidArgumentError(
				"update_time cannot be updated",
				"The update_time field is read-only and cannot be changed",
			)
		}
	}
	return nil
}

// UpdatePolicy updates an existing policy using partial merge (PATCH).
func (s *PolicyServiceImpl) UpdatePolicy(ctx context.Context, id string, patch *v1alpha1.Policy) (*v1alpha1.Policy, error) {
	log := logging.FromContext(ctx)
	log.Debug("Updating policy", "policy_id", id)

	if err := validatePatchInput(patch); err != nil {
		return nil, err
	}

	existingDB, err := s.store.Policy().Get(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrPolicyNotFound) {
			return nil, NewPolicyNotFoundError(id)
		}
		log.Error("Failed to get existing policy for update", "policy_id", id, "error", err)
		return nil, NewInternalError("Failed to get existing policy", err.Error(), err)
	}
	existing := DBToAPIModel(existingDB)
	if err := validatePatchImmutableFields(patch, existing); err != nil {
		return nil, err
	}
	merged := mergePolicyOntoPolicy(patch, existing)

	// If RegoCode is being updated, validate it
	regoChanged := patch != nil && patch.RegoCode != nil
	if regoChanged {
		// Validate Rego via engine
		if err := s.engine.ValidateRego(ctx, *patch.RegoCode); err != nil {
			return nil, handleEngineError(err, "update")
		}

		log.Debug("Rego code validated", "policy_id", id)
	}

	// Save the existing DB state for potential rollback
	previousDB := *existingDB

	// Convert API model to DB model and update store
	dbPolicy := APIToDBModel(merged, id)
	updated, err := s.store.Policy().Update(ctx, dbPolicy)
	if err != nil {
		log.Error("Failed to update policy in store", "policy_id", id, "error", err)
		return nil, processPolicyStoreError(err, dbPolicy, "update")
	}

	// Recompile engine if Rego changed
	if regoChanged {
		if err := s.recompileEngine(ctx); err != nil {
			log.Error("Failed to recompile engine after update, rolling back DB", "policy_id", id, "error", err)
			// Rollback: restore previous DB state
			if _, rollbackErr := s.rollbackPolicyUpdate(previousDB); rollbackErr != nil {
				log.Error("Failed to rollback DB policy after compile failure",
					"policy_id", id,
					"db_error", rollbackErr,
					"compile_error", err)
			}
			return nil, NewInternalError("Failed to compile policies after update", err.Error(), err)
		}
	}

	// Convert back to API model
	apiPolicy := DBToAPIModel(updated)

	log.Debug("Policy updated successfully", "policy_id", id)
	return &apiPolicy, nil
}

// DeletePolicy deletes a policy by ID.
func (s *PolicyServiceImpl) DeletePolicy(ctx context.Context, id string) error {
	log := logging.FromContext(ctx)
	log.Debug("Deleting policy", "policy_id", id)

	// Delete policy from store
	err := s.store.Policy().Delete(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrPolicyNotFound) {
			return NewPolicyNotFoundError(id)
		}
		log.Error("Failed to delete policy from store", "policy_id", id, "error", err)
		return NewInternalError("Failed to delete policy", err.Error(), err)
	}

	// Recompile engine without the deleted policy
	if err := s.recompileEngine(ctx); err != nil {
		log.Warn("Failed to recompile engine after delete", "policy_id", id, "error", err)
	}

	log.Debug("Policy deleted successfully", "policy_id", id)
	return nil
}

func (s *PolicyServiceImpl) rollbackPolicyDelete(id string) error {
	rbCtx, cancel := context.WithTimeout(context.Background(), policyRollbackTimeout)
	defer cancel()
	return s.store.Policy().Delete(rbCtx, id)
}

func (s *PolicyServiceImpl) rollbackPolicyUpdate(previous model.Policy) (*model.Policy, error) {
	rbCtx, cancel := context.WithTimeout(context.Background(), policyRollbackTimeout)
	defer cancel()
	return s.store.Policy().Update(rbCtx, previous)
}
