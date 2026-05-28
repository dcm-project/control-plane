package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/dcm-project/control-plane/internal/catalog/store/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrCatalogItemNotFound is returned when a catalog item is not found
	ErrCatalogItemNotFound = errors.New("catalog item not found")
	// ErrCatalogItemIDTaken is returned when a catalog item ID is already taken
	ErrCatalogItemIDTaken = errors.New("catalog item ID already exists")
	// ErrCatalogItemHasInstances is returned when attempting to delete a catalog item with existing instances
	ErrCatalogItemHasInstances = errors.New("cannot delete catalog item with existing instances")
)

// CatalogItemListOptions contains options for listing catalog items
type CatalogItemListOptions struct {
	PageToken   *string
	PageSize    int
	ServiceType *string
}

// CatalogItemListResult contains the result of a List operation
type CatalogItemListResult struct {
	CatalogItems  model.CatalogItemList
	NextPageToken *string
}

// CatalogItemStore defines operations for CatalogItem resources
type CatalogItemStore interface {
	List(ctx context.Context, opts *CatalogItemListOptions) (*CatalogItemListResult, error)
	Create(ctx context.Context, catalogItem model.CatalogItem) (*model.CatalogItem, error)
	Get(ctx context.Context, id string) (*model.CatalogItem, error)
	Update(ctx context.Context, catalogItem *model.CatalogItem) error
	Delete(ctx context.Context, id string) error
	SeedIfEmpty(ctx context.Context, items []model.CatalogItem) error
}

type catalogItemStore struct {
	db     *gorm.DB
	logger *slog.Logger
}

// NewCatalogItemStore creates a new CatalogItem store
func NewCatalogItemStore(db *gorm.DB, logger *slog.Logger) CatalogItemStore {
	return &catalogItemStore{db: db, logger: logger}
}

// List returns a paginated list of catalog items
func (s *catalogItemStore) List(ctx context.Context, opts *CatalogItemListOptions) (*CatalogItemListResult, error) {
	var catalogItems model.CatalogItemList
	query := s.db.WithContext(ctx)

	// Default max page size
	pageSize := 100
	if opts != nil && opts.PageSize > 0 {
		pageSize = opts.PageSize
	}

	offset := 0
	if opts != nil && opts.PageToken != nil && *opts.PageToken != "" {
		var err error
		offset, err = decodePageToken(*opts.PageToken)
		if err != nil {
			return nil, err
		}
	}

	query = query.Order("id ASC").Limit(pageSize + 1).Offset(offset)
	if opts != nil && opts.ServiceType != nil && *opts.ServiceType != "" {
		query = query.Where("spec_service_type = ?", *opts.ServiceType)
	}

	if err := query.Find(&catalogItems).Error; err != nil {
		return nil, err
	}

	result := &CatalogItemListResult{
		CatalogItems: catalogItems,
	}
	if len(catalogItems) > pageSize {
		result.CatalogItems = catalogItems[:pageSize]
		nextOffset := offset + pageSize
		nextPageToken, err := encodePageToken(nextOffset)
		if err != nil {
			return nil, err
		}
		result.NextPageToken = &nextPageToken
	}
	return result, nil
}

// Create creates a new catalog item
func (s *catalogItemStore) Create(ctx context.Context, catalogItem model.CatalogItem) (*model.CatalogItem, error) {
	catalogItem.SpecServiceType = catalogItem.Spec.ServiceType
	if err := s.db.WithContext(ctx).Clauses(clause.Returning{}).Create(&catalogItem).Error; err != nil {
		return nil, s.mapConstraintError(ctx, err, catalogItem)
	}
	return &catalogItem, nil
}

// mapConstraintError maps a DB constraint violation to a store sentinel error
func (s *catalogItemStore) mapConstraintError(ctx context.Context, err error, attempted model.CatalogItem) error {
	if err == nil {
		return nil
	}

	errStr := strings.ToLower(err.Error())

	// Check for foreign key violation first (before checking for generic constraint failed)
	if strings.Contains(errStr, "foreign key") {
		// Verify which constraint failed by checking if service type exists
		var st model.ServiceType
		if err := s.db.WithContext(ctx).Where("service_type = ?", attempted.SpecServiceType).First(&st).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrServiceTypeNotFound
			}
		}
		return err
	}

	// Handle unique constraint violations
	if errors.Is(err, gorm.ErrDuplicatedKey) ||
		strings.Contains(errStr, "unique") ||
		strings.Contains(errStr, "duplicate key") {
		var row model.CatalogItem
		dberr := s.db.WithContext(ctx).Where("id = ?", attempted.ID).Limit(1).First(&row).Error
		if dberr == nil {
			return ErrCatalogItemIDTaken
		}
		if !errors.Is(dberr, gorm.ErrRecordNotFound) {
			return err
		}
	}

	return err
}

// Get retrieves a catalog item by ID
func (s *catalogItemStore) Get(ctx context.Context, id string) (*model.CatalogItem, error) {
	var catalogItem model.CatalogItem
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&catalogItem).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCatalogItemNotFound
		}
		return nil, fmt.Errorf("failed to get catalog item: %w", err)
	}
	return &catalogItem, nil
}

// Update updates a catalog item (only mutable fields)
func (s *catalogItemStore) Update(ctx context.Context, catalogItem *model.CatalogItem) error {
	// Extract service type from spec for denormalized field
	catalogItem.SpecServiceType = catalogItem.Spec.ServiceType

	result := s.db.WithContext(ctx).Model(&model.CatalogItem{}).
		Where("id = ?", catalogItem.ID).
		Select("display_name", "spec", "spec_service_type").
		Updates(catalogItem)

	if result.Error != nil {
		return s.mapConstraintError(ctx, result.Error, *catalogItem)
	}
	if result.RowsAffected == 0 {
		return ErrCatalogItemNotFound
	}
	return nil
}

// Delete deletes a catalog item by ID
func (s *catalogItemStore) Delete(ctx context.Context, id string) error {
	result := s.db.WithContext(ctx).Where("id = ?", id).Delete(&model.CatalogItem{})
	if result.Error != nil {
		// Check for foreign key violation (instances exist)
		errStr := strings.ToLower(result.Error.Error())
		if strings.Contains(errStr, "foreign key") {
			return ErrCatalogItemHasInstances
		}
		return fmt.Errorf("failed to delete catalog item: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrCatalogItemNotFound
	}
	return nil
}

// SeedIfEmpty inserts the given catalog items if the table has no rows.
// Uses a transaction to avoid races when multiple instances start concurrently.
func (s *catalogItemStore) SeedIfEmpty(ctx context.Context, items []model.CatalogItem) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&model.CatalogItem{}).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		var inserted int64
		for _, m := range items {
			result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoNothing: true}).Create(&m)
			if err := result.Error; err != nil {
				return err
			}
			inserted += result.RowsAffected
		}
		if inserted > 0 {
			s.logger.InfoContext(ctx, "Seeded default catalog items", "count", inserted)
		}
		return nil
	})
}
