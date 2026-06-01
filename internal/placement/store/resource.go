package store

import (
	"context"
	"errors"
	"strings"

	"github.com/dcm-project/control-plane/internal/placement/store/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrResourceNotFound = errors.New("resource not found")
	ErrResourceIdExist  = errors.New("resource with id already exists")
)

// ResourceListOptions contains optional fields for listing requests.
type ResourceListOptions struct {
	ProviderName *string
	PageSize     int
	PageToken    *string
}

// ResourceListResult contains the result of a List operation.
type ResourceListResult struct {
	Resources     model.ResourceList
	NextPageToken *string
}

// Resource defines the repository interface for Resource operations
type Resource interface {
	List(ctx context.Context, opts *ResourceListOptions) (*ResourceListResult, error)
	Create(ctx context.Context, request model.Resource) (*model.Resource, error)
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*model.Resource, error)
}

type ResourceStore struct {
	db *gorm.DB
}

var _ Resource = (*ResourceStore)(nil)

// NewResource creates a new Resource repository
func NewResource(db *gorm.DB) Resource {
	return &ResourceStore{db: db}
}

func (s *ResourceStore) List(ctx context.Context, opts *ResourceListOptions) (*ResourceListResult, error) {
	var requests model.ResourceList
	query := s.db.WithContext(ctx)

	// Default page size
	pageSize := 100
	if opts != nil && opts.PageSize > 0 {
		pageSize = opts.PageSize
	}

	// Decode page token to get offset
	offset := 0
	if opts != nil {
		offset = decodePageToken(opts.PageToken)
	}

	// Apply filters
	if opts != nil {
		if opts.ProviderName != nil && *opts.ProviderName != "" {
			query = query.Where("provider_name = ?", *opts.ProviderName)
		}
	}

	// Apply consistent ordering for pagination
	query = query.Order("create_time ASC, id ASC")

	// Query with limit+1 to detect if there are more results
	query = query.Limit(pageSize + 1).Offset(offset)

	if err := query.Find(&requests).Error; err != nil {
		return nil, err
	}

	// Build result with next page token if needed
	result := &ResourceListResult{
		Resources:     requests,
		NextPageToken: generateNextPageToken(len(requests), pageSize, offset),
	}

	// Trim to requested page size if we got limit+1 results
	if len(requests) > pageSize {
		result.Resources = requests[:pageSize]
	}

	return result, nil
}

func (s *ResourceStore) Create(ctx context.Context, request model.Resource) (*model.Resource, error) {
	if err := s.db.WithContext(ctx).Clauses(clause.Returning{}).Create(&request).Error; err != nil {
		// Check for duplicate key errors (PostgreSQL, MySQL, SQLite)
		errMsg := err.Error()
		if errors.Is(err, gorm.ErrDuplicatedKey) ||
			strings.Contains(errMsg, "UNIQUE constraint") ||
			strings.Contains(errMsg, "duplicate key") {
			return nil, ErrResourceIdExist
		}
		return nil, err
	}
	return &request, nil
}

func (s *ResourceStore) Delete(ctx context.Context, id string) error {
	result := s.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Resource{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrResourceNotFound
	}
	return nil
}

func (s *ResourceStore) Get(ctx context.Context, id string) (*model.Resource, error) {
	var request model.Resource
	if err := s.db.WithContext(ctx).First(&request, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}
	return &request, nil
}
