package service

import (
	"maps"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

// FieldConfigurationsToModel converts API field configurations to store model.
func FieldConfigurationsToModel(f []v1alpha1.FieldConfiguration) []model.FieldConfiguration {
	if f == nil {
		return nil
	}
	out := make([]model.FieldConfiguration, len(f))
	for i := range f {
		out[i] = fieldConfigurationToModel(f[i])
	}
	return out
}

func fieldConfigurationToModel(f v1alpha1.FieldConfiguration) model.FieldConfiguration {
	var displayName string
	if f.DisplayName != nil {
		displayName = *f.DisplayName
	}
	var vs map[string]any
	if f.ValidationSchema != nil {
		vs = make(map[string]any)
		maps.Copy(vs, *f.ValidationSchema)
	}
	var dep *model.DependsOn
	if f.DependsOn != nil {
		dep = dependsOnAPIToModel(f.DependsOn)
	}
	return model.FieldConfiguration{
		Path:             f.Path,
		DisplayName:      displayName,
		Editable:         f.Editable != nil && *f.Editable,
		Default:          f.Default,
		ValidationSchema: vs,
		DependsOn:        dep,
	}
}

func dependsOnAPIToModel(d *v1alpha1.FieldConfigurationDependsOn) *model.DependsOn {
	av := make(map[string][]any)
	maps.Copy(av, d.AllowedValues)
	return &model.DependsOn{Path: d.Path, AllowedValues: av}
}

// FieldConfigurationsFromModel converts store field configurations to API types.
// Deep-copies ValidationSchema and DependsOn.AllowedValues to avoid shared references.
func FieldConfigurationsFromModel(m []model.FieldConfiguration) []v1alpha1.FieldConfiguration {
	if m == nil {
		return nil
	}
	out := make([]v1alpha1.FieldConfiguration, len(m))
	for i := range m {
		out[i] = fieldConfigurationFromModel(m[i])
	}
	return out
}

func fieldConfigurationFromModel(f model.FieldConfiguration) v1alpha1.FieldConfiguration {
	out := v1alpha1.FieldConfiguration{
		Path:    f.Path,
		Default: f.Default,
	}
	if f.DisplayName != "" {
		displayName := f.DisplayName
		out.DisplayName = &displayName
	}
	if f.Editable {
		editable := true
		out.Editable = &editable
	}
	if f.ValidationSchema != nil {
		vs := make(map[string]any)
		maps.Copy(vs, f.ValidationSchema)
		out.ValidationSchema = &vs
	}
	if f.DependsOn != nil {
		av := make(map[string][]any)
		maps.Copy(av, f.DependsOn.AllowedValues)
		out.DependsOn = &v1alpha1.FieldConfigurationDependsOn{Path: f.DependsOn.Path, AllowedValues: av}
	}
	return out
}
