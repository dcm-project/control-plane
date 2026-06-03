package store

import (
	"context"
	"errors"
	"strings"

	"github.com/dcm-project/control-plane/internal/policy/store/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrPolicyNotFound             = errors.New("policy not found")
	ErrPolicyIDTaken              = errors.New("policy ID already taken")
	ErrDisplayNamePolicyTypeTaken = errors.New("display_name and policy_type combination already taken")
	ErrPriorityPolicyTypeTaken    = errors.New("priority and policy_type combination already taken")
)

// PolicyFilter contains optional fields for filtering policy queries.
// nil fields are ignored (not filtered).
type PolicyFilter struct {
	PolicyType *string
	Enabled    *bool
}

// PolicyListOptions contains options for listing policies.
type PolicyListOptions struct {
	Filter    *PolicyFilter
	OrderBy   string
	PageToken *string
	PageSize  int
}

// PolicyListResult contains the result of a List operation.
type PolicyListResult struct {
	Policies      model.PolicyList
	NextPageToken string
}

type Policy interface {
	List(ctx context.Context, opts *PolicyListOptions) (*PolicyListResult, error)
	ListAll(ctx context.Context) (model.PolicyList, error)
	Create(ctx context.Context, policy model.Policy) (*model.Policy, error)
	Delete(ctx context.Context, id string) error
	Update(ctx context.Context, policy model.Policy) (*model.Policy, error)
	Get(ctx context.Context, id string) (*model.Policy, error)
}

type PolicyStore struct {
	db *gorm.DB
}

var _ Policy = (*PolicyStore)(nil)

func NewPolicy(db *gorm.DB) Policy {
	return &PolicyStore{db: db}
}

func (s *PolicyStore) List(ctx context.Context, opts *PolicyListOptions) (*PolicyListResult, error) {
	var policies model.PolicyList
	query := s.db.WithContext(ctx)

	// Default page size
	pageSize := 50
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

	if opts != nil {
		if opts.Filter != nil {
			if opts.Filter.PolicyType != nil {
				query = query.Where("policy_type = ?", *opts.Filter.PolicyType)
			}
			if opts.Filter.Enabled != nil {
				query = query.Where("enabled = ?", *opts.Filter.Enabled)
			}
		}

		// Apply ordering
		if opts.OrderBy != "" {
			query = query.Order(opts.OrderBy)
		} else {
			// Default order by policy_type, priority, id ascending
			query = query.Order("policy_type ASC, priority ASC, id ASC")
		}
	} else {
		// Default order when no options provided
		query = query.Order("policy_type ASC, priority ASC, id ASC")
	}

	// Query with limit+1 to detect if there are more results
	query = query.Limit(pageSize + 1).Offset(offset)

	if err := query.Find(&policies).Error; err != nil {
		return nil, err
	}

	// Generate next page token if there are more results
	result := &PolicyListResult{
		Policies: policies,
	}

	if len(policies) > pageSize {
		// Trim to requested page size
		result.Policies = policies[:pageSize]
		nextOffset := offset + pageSize
		nextPageToken, err := encodePageToken(nextOffset)
		if err != nil {
			return nil, err
		}
		result.NextPageToken = nextPageToken
	}

	return result, nil
}

// mapUniqueConstraintError maps a DB unique constraint violation to a store sentinel error.
// by querying the DB to see which constraint would be violated (ID, display_name+policy_type, or priority+policy_type).
func (s *PolicyStore) mapUniqueConstraintError(ctx context.Context, err error, attempted model.Policy, isUpdate bool) error {
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		// Raw driver error (e.g. tests without TranslateError)
		if !strings.Contains(strings.ToLower(err.Error()), "unique") &&
			!strings.Contains(err.Error(), "duplicate key") {
			return err
		}
	}

	checks := []struct {
		sentinel error
		query    *gorm.DB
	}{
		{ErrPolicyIDTaken, s.db.WithContext(ctx).Where("id = ?", attempted.ID).Limit(1)},
		{ErrDisplayNamePolicyTypeTaken, s.db.WithContext(ctx).Where("display_name = ? AND policy_type = ?", attempted.DisplayName, attempted.PolicyType).Limit(1)},
		{ErrPriorityPolicyTypeTaken, s.db.WithContext(ctx).Where("priority = ? AND policy_type = ?", attempted.Priority, attempted.PolicyType).Limit(1)},
	}

	for _, c := range checks {
		query := c.query
		if isUpdate {
			query = query.Where("id != ?", attempted.ID)
		}
		var row model.Policy
		dberr := query.First(&row).Error
		if dberr == nil {
			return c.sentinel
		}
		if !errors.Is(dberr, gorm.ErrRecordNotFound) {
			return err
		}
	}

	return err
}

func (s *PolicyStore) Create(ctx context.Context, policy model.Policy) (*model.Policy, error) {
	if err := s.db.WithContext(ctx).Clauses(clause.Returning{}).Select("*").Create(&policy).Error; err != nil {
		return nil, s.mapUniqueConstraintError(ctx, err, policy, false)
	}
	return &policy, nil
}

func (s *PolicyStore) Delete(ctx context.Context, id string) error {
	result := s.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Policy{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrPolicyNotFound
	}
	return nil
}

func (s *PolicyStore) Update(ctx context.Context, policy model.Policy) (*model.Policy, error) {
	// Use Select to update all mutable fields including zero values
	// Immutable fields (id, policy_type, create_time) are not updated
	result := s.db.WithContext(ctx).Model(&policy).
		Select("display_name", "description", "label_selector", "priority", "rego_code", "enabled").
		Clauses(clause.Returning{}).
		Updates(&policy)
	if result.Error != nil {
		return nil, s.mapUniqueConstraintError(ctx, result.Error, policy, true)
	}
	if result.RowsAffected == 0 {
		return nil, ErrPolicyNotFound
	}
	return &policy, nil
}

func (s *PolicyStore) ListAll(ctx context.Context) (model.PolicyList, error) {
	var policies model.PolicyList
	if err := s.db.WithContext(ctx).Order("id ASC").Find(&policies).Error; err != nil {
		return nil, err
	}
	if policies == nil {
		policies = model.PolicyList{}
	}
	return policies, nil
}

func (s *PolicyStore) Get(ctx context.Context, id string) (*model.Policy, error) {
	var policy model.Policy
	if err := s.db.WithContext(ctx).First(&policy, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPolicyNotFound
		}
		return nil, err
	}
	return &policy, nil
}
