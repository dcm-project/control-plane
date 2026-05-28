package service

import "errors"

// Domain errors for the service layer
var (
	// ErrInvalidServiceType indicates the service type is not one of the allowed values (vm, container, cluster, database)
	ErrInvalidServiceType = errors.New("invalid service type: must be one of: vm, container, cluster, database")

	// ErrServiceTypeIDTaken indicates a service type with the given ID already exists
	ErrServiceTypeIDTaken = errors.New("service type ID already exists")

	// ErrServiceTypeNameTaken indicates a service type with the given service_type value already exists
	ErrServiceTypeNameTaken = errors.New("service type name already taken")

	// ErrServiceTypeNotFound indicates the requested service type does not exist
	ErrServiceTypeNotFound = errors.New("service type not found")

	// ErrCatalogItemNotFound indicates the requested catalog item does not exist
	ErrCatalogItemNotFound = errors.New("catalog item not found")

	// ErrCatalogItemIDTaken indicates a catalog item with the given ID already exists
	ErrCatalogItemIDTaken = errors.New("catalog item ID already exists")

	// ErrCatalogItemHasInstances indicates a catalog item has existing instances
	ErrCatalogItemHasInstances = errors.New("catalog item has existing instances")

	// ErrImmutableFieldUpdate indicates an attempt to change api_version or spec.service_type
	ErrImmutableFieldUpdate = errors.New("cannot update immutable fields: api_version and spec.service_type are immutable")

	// ErrCatalogItemInstanceNotFound indicates the requested catalog item instance does not exist
	ErrCatalogItemInstanceNotFound = errors.New("catalog item instance not found")

	// ErrCatalogItemInstanceIDTaken indicates a catalog item instance with the given ID already exists
	ErrCatalogItemInstanceIDTaken = errors.New("catalog item instance ID already exists")

	// ErrCatalogItemNotFoundForInstance indicates the catalog item referenced by the instance does not exist
	ErrCatalogItemNotFoundForInstance = errors.New("referenced catalog item does not exist")

	// ErrUserValuePathNotFound indicates a user_value path does not match any CatalogItem field
	ErrUserValuePathNotFound = errors.New("user value path not found in catalog item fields")

	// ErrUserValueNotEditable indicates the field at the given path is not editable
	ErrUserValueNotEditable = errors.New("field is not editable")

	// ErrUserValueValidationFailed indicates the user value failed validation against the field's validation_schema
	ErrUserValueValidationFailed = errors.New("user value validation failed")

	// ErrFieldDefaultValidationFailed indicates a catalog item field default failed validation_schema
	ErrFieldDefaultValidationFailed = errors.New("field default validation failed")

	// ErrDependsOnCycleDetected indicates the catalog item's field configurations contain a cyclic depends_on reference
	ErrDependsOnCycleDetected = errors.New("depends_on cycle detected in field configurations")

	// ErrDependsOnPathNotFound indicates a depends_on path does not reference any field in the catalog item
	ErrDependsOnPathNotFound = errors.New("depends_on path does not reference an existing field")

	// ErrUserValueDependsOnViolation indicates the user value is not allowed given the current value of the field it depends on
	ErrUserValueDependsOnViolation = errors.New("user value violates depends_on constraint")

	// ErrPlacementManagerPolicyRejected indicates the Placement Manager rejected the request due to policy (406)
	ErrPlacementManagerPolicyRejected = errors.New("placement manager request rejected by policy engine")

	// ErrPlacementManagerProviderError indicates the Placement Manager SPRM provider returned an error (422)
	ErrPlacementManagerProviderError = errors.New("placement manager provider error")

	// ErrPlacementManagerPolicyDependency indicates policy evaluation succeeded but a required dependency was not satisfied (424)
	ErrPlacementManagerPolicyDependency = errors.New("placement manager policy dependency not satisfied")

	// ErrPlacementManagerCreateFailed indicates the Placement Manager failed to create a resource
	ErrPlacementManagerCreateFailed = errors.New("placement manager create resource failed")

	// ErrPlacementManagerDeleteFailed indicates the Placement Manager failed to delete a resource
	ErrPlacementManagerDeleteFailed = errors.New("placement manager delete resource failed")

	// ErrPlacementManagerRehydrateFailed indicates the Placement Manager failed to rehydrate a resource
	ErrPlacementManagerRehydrateFailed = errors.New("placement manager rehydrate resource failed")

	// ErrCatalogItemInstanceConflict indicates a concurrent modification was detected
	ErrCatalogItemInstanceConflict = errors.New("catalog item instance was modified concurrently")
)
