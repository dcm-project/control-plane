// Package opa provides an embedded OPA engine for policy compilation and evaluation.
package opa

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
)

// Engine defines the interface for the embedded OPA engine
type Engine interface {
	// Compile loads and compiles all Rego modules, replacing any previously compiled state.
	Compile(ctx context.Context, policies []PolicyModule) error

	// EvaluatePolicy evaluates a policy by ID against the given input.
	EvaluatePolicy(ctx context.Context, policyID string, input map[string]any) (*EvaluationResult, error)

	// ValidateRego checks Rego syntax without persisting.
	ValidateRego(ctx context.Context, regoCode string) error
}

// PolicyModule represents a Rego module to compile
type PolicyModule struct {
	ID       string
	RegoCode string
}

// embeddedEngine implements Engine using OPA's Go library
type embeddedEngine struct {
	mu        sync.RWMutex // protects reads/writes of queries
	compileMu sync.Mutex   // serializes Compile calls
	queries   map[string]*rego.PreparedEvalQuery
}

// NewEngine creates a new embedded OPA engine
func NewEngine() Engine {
	return &embeddedEngine{}
}

// Compile compiles all provided policy modules. On success, replaces the previous compiled state.
// On failure, the previous state is preserved (atomic). Concurrent Compile calls are serialized.
func (e *embeddedEngine) Compile(ctx context.Context, policies []PolicyModule) error {
	e.compileMu.Lock()
	defer e.compileMu.Unlock()

	if len(policies) == 0 {
		e.mu.Lock()
		e.queries = nil
		e.mu.Unlock()
		return nil
	}

	// Build source map for compilation
	sources := make(map[string]string, len(policies))
	for _, p := range policies {
		sources[p.ID] = p.RegoCode
	}

	// Compile all modules together to catch cross-module errors
	compiler, err := ast.CompileModulesWithOpt(sources, ast.CompileOpts{ParserOptions: ast.ParserOptions{RegoVersion: ast.RegoV1}})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRego, err)
	}

	// Build one PreparedEvalQuery per policy, keyed by policy ID
	newQueries := make(map[string]*rego.PreparedEvalQuery, len(policies))
	for _, p := range policies {
		mod := compiler.Modules[p.ID]
		// mod.Package.Path is like "data.policies.my_policy", we need the part after "data."
		pkgName := strings.TrimPrefix(mod.Package.Path.String(), "data.")
		query := fmt.Sprintf("data.%s.main", pkgName)

		r := rego.New(
			rego.Query(query),
			rego.Compiler(compiler),
		)
		pq, err := r.PrepareForEval(ctx)
		if err != nil {
			return fmt.Errorf("%w: failed to prepare query for policy '%s': %v", ErrInvalidRego, p.ID, err)
		}
		newQueries[p.ID] = &pq
	}

	// Atomically swap the query map
	e.mu.Lock()
	e.queries = newQueries
	e.mu.Unlock()

	return nil
}

// EvaluatePolicy evaluates a policy by ID. Safe for concurrent use.
func (e *embeddedEngine) EvaluatePolicy(ctx context.Context, policyID string, input map[string]any) (*EvaluationResult, error) {
	e.mu.RLock()
	pq, ok := e.queries[policyID]
	e.mu.RUnlock()

	if !ok {
		return &EvaluationResult{Defined: false}, nil
	}

	rs, err := pq.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return nil, fmt.Errorf("evaluation error for policy '%s': %w", policyID, err)
	}

	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return &EvaluationResult{Defined: false}, nil
	}

	val := rs[0].Expressions[0].Value
	if val == nil {
		return &EvaluationResult{Defined: false}, nil
	}

	resultMap, ok := val.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: policy main rule must return an object; got %T", ErrEngineInternal, val)
	}

	return &EvaluationResult{
		Result:  resultMap,
		Defined: true,
	}, nil
}

// ValidateRego checks that the given Rego code compiles without errors.
func (e *embeddedEngine) ValidateRego(_ context.Context, regoCode string) error {
	if strings.TrimSpace(regoCode) == "" {
		return fmt.Errorf("%w: empty Rego code", ErrInvalidRego)
	}

	_, err := ast.ParseModuleWithOpts("validation", regoCode, ast.ParserOptions{RegoVersion: ast.RegoV1})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRego, err)
	}

	return nil
}
