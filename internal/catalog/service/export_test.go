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

// BuildResourceGraph delegates to the unexported specBuilder.
func (b *SpecBuilder) BuildResourceGraph(ctx context.Context, catalogItemId string, userValues []v1alpha1.UserValue) ([]ResolvedResource, error) {
	return b.inner.BuildResourceGraph(ctx, catalogItemId, userValues)
}
