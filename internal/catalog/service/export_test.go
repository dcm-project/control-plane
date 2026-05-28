package service

import (
	"context"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/store"
)

// SpecBuilder is an exported wrapper for testing specBuilder from external test packages.
type SpecBuilder struct {
	inner *specBuilder
}

// NewSpecBuilderForTest creates an exported SpecBuilder for external test packages.
func NewSpecBuilderForTest(s store.Store) *SpecBuilder {
	return &SpecBuilder{inner: newSpecBuilder(s)}
}

// BuildResourceSpec delegates to the unexported specBuilder.
func (b *SpecBuilder) BuildResourceSpec(ctx context.Context, catalogItemId string, userValues []v1alpha1.UserValue) (map[string]any, error) {
	return b.inner.BuildResourceSpec(ctx, catalogItemId, userValues)
}
