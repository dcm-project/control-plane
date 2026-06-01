package service

import (
	"fmt"

	"github.com/dcm-project/control-plane/api/policy/v1alpha1"
	"github.com/dcm-project/control-plane/internal/policy/store/model"
)

const (
	DefaultPriority = 500
)

// APIToDBModel converts an API Policy model to a database Policy model.
// All Policy fields are optional in the schema; required fields for create are enforced by the service.
func APIToDBModel(api v1alpha1.Policy, id string) model.Policy {
	db := model.Policy{ID: id}

	if api.DisplayName != nil {
		db.DisplayName = *api.DisplayName
	}
	if api.PolicyType != nil {
		db.PolicyType = string(*api.PolicyType)
	}

	if api.Description != nil {
		db.Description = *api.Description
	}
	if api.Enabled != nil {
		db.Enabled = *api.Enabled
	} else {
		db.Enabled = true
	}
	if api.Priority != nil {
		db.Priority = *api.Priority
	} else {
		db.Priority = DefaultPriority
	}
	if api.LabelSelector != nil {
		db.LabelSelector = *api.LabelSelector
	}
	if api.RegoCode != nil {
		db.RegoCode = *api.RegoCode
	}

	return db
}

// DBToAPIModel converts a database Policy model to an API Policy model.
func DBToAPIModel(db *model.Policy) v1alpha1.Policy {
	path := fmt.Sprintf("policies/%s", db.ID)
	displayName := db.DisplayName
	policyType := v1alpha1.PolicyPolicyType(db.PolicyType)
	api := v1alpha1.Policy{
		Id:          &db.ID,
		Path:        &path,
		DisplayName: &displayName,
		PolicyType:  &policyType,
		Priority:    &db.Priority,
		Enabled:     &db.Enabled,
		CreateTime:  &db.CreateTime,
		UpdateTime:  &db.UpdateTime,
		RegoCode:    &db.RegoCode,
	}
	if db.Description != "" {
		api.Description = &db.Description
	}
	if len(db.LabelSelector) > 0 {
		api.LabelSelector = &db.LabelSelector
	}
	return api
}
