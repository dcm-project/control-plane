package opa_test

import (
	"context"
	"errors"
	"sync"

	"github.com/dcm-project/control-plane/internal/policy/opa"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Engine", func() {
	var (
		engine opa.Engine
		ctx    context.Context
	)

	BeforeEach(func() {
		engine = opa.NewEngine()
		ctx = context.Background()
	})

	Describe("Compile", func() {
		It("compiles valid policies", func() {
			modules := []opa.PolicyModule{
				{ID: "p1", RegoCode: "package policy_a\nmain = {\"rejected\": false}"},
				{ID: "p2", RegoCode: "package policy_b\nmain = {\"rejected\": false}"},
				{ID: "p3", RegoCode: "package policy_c\nmain = {\"rejected\": false}"},
			}

			err := engine.Compile(ctx, modules)
			Expect(err).NotTo(HaveOccurred())
		})

		It("compiles zero policies", func() {
			err := engine.Compile(ctx, []opa.PolicyModule{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns ErrInvalidRego for invalid Rego", func() {
			modules := []opa.PolicyModule{
				{ID: "bad", RegoCode: "package test\n{invalid"},
			}

			err := engine.Compile(ctx, modules)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, opa.ErrInvalidRego)).To(BeTrue())
		})

		It("replaces previous policies", func() {
			// Compile policy A
			err := engine.Compile(ctx, []opa.PolicyModule{
				{ID: "a", RegoCode: "package policy_a\nmain = {\"rejected\": false}"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Compile only policy B (no policy A)
			err = engine.Compile(ctx, []opa.PolicyModule{
				{ID: "b", RegoCode: "package policy_b\nmain = {\"rejected\": false}"},
			})
			Expect(err).NotTo(HaveOccurred())

			// A should be undefined (looked up by ID)
			resultA, err := engine.EvaluatePolicy(ctx, "a", map[string]any{})
			Expect(err).NotTo(HaveOccurred())
			Expect(resultA.Defined).To(BeFalse())

			// B should be defined (looked up by ID)
			resultB, err := engine.EvaluatePolicy(ctx, "b", map[string]any{})
			Expect(err).NotTo(HaveOccurred())
			Expect(resultB.Defined).To(BeTrue())
		})

		It("is atomic on failure", func() {
			// Compile valid policies
			err := engine.Compile(ctx, []opa.PolicyModule{
				{ID: "valid", RegoCode: "package valid_policy\nmain = {\"rejected\": false}"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Try to compile with one invalid policy
			err = engine.Compile(ctx, []opa.PolicyModule{
				{ID: "still-valid", RegoCode: "package still_valid\nmain = {\"rejected\": false}"},
				{ID: "bad", RegoCode: "package bad\n{invalid"},
			})
			Expect(err).To(HaveOccurred())

			// Previous valid policy should still be evaluable (looked up by ID)
			result, err := engine.EvaluatePolicy(ctx, "valid", map[string]any{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Defined).To(BeTrue())
		})
	})

	Describe("EvaluatePolicy", func() {
		It("returns decision", func() {
			err := engine.Compile(ctx, []opa.PolicyModule{
				{ID: "test", RegoCode: "package test\nmain = {\"rejected\": false, \"patch\": {\"foo\": \"bar\"}}"},
			})
			Expect(err).NotTo(HaveOccurred())

			result, err := engine.EvaluatePolicy(ctx, "test", map[string]any{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Defined).To(BeTrue())
			Expect(result.Result["rejected"]).To(BeFalse())
			Expect(result.Result["patch"]).To(Equal(map[string]any{"foo": "bar"}))
		})

		It("returns decision when ID differs from package name", func() {
			err := engine.Compile(ctx, []opa.PolicyModule{
				{ID: "my-policy-id", RegoCode: "package some_other_name\nmain = {\"rejected\": false}"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Lookup by ID, not package name
			result, err := engine.EvaluatePolicy(ctx, "my-policy-id", map[string]any{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Defined).To(BeTrue())

			// Package name should not work as lookup key
			result, err = engine.EvaluatePolicy(ctx, "some_other_name", map[string]any{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Defined).To(BeFalse())
		})

		It("returns undefined for non-matching condition", func() {
			err := engine.Compile(ctx, []opa.PolicyModule{
				{ID: "cond", RegoCode: "package cond\nmain = {\"rejected\": false} if {\n  input.x == \"yes\"\n}"},
			})
			Expect(err).NotTo(HaveOccurred())

			result, err := engine.EvaluatePolicy(ctx, "cond", map[string]any{"x": "no"})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Defined).To(BeFalse())
		})

		It("returns undefined for non-existent ID", func() {
			err := engine.Compile(ctx, []opa.PolicyModule{
				{ID: "test", RegoCode: "package test\nmain = {\"rejected\": false}"},
			})
			Expect(err).NotTo(HaveOccurred())

			result, err := engine.EvaluatePolicy(ctx, "other", map[string]any{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Defined).To(BeFalse())
		})

		It("handles complex input", func() {
			regoCode := `package complex
main = {"rejected": false, "patch": {"cpu_limit": input.spec.cpu}} if {
  input.spec.cpu == "2"
}`
			err := engine.Compile(ctx, []opa.PolicyModule{
				{ID: "complex-policy", RegoCode: regoCode},
			})
			Expect(err).NotTo(HaveOccurred())

			result, err := engine.EvaluatePolicy(ctx, "complex-policy", map[string]any{
				"spec": map[string]any{"cpu": "2"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Defined).To(BeTrue())
			Expect(result.Result["patch"]).To(Equal(map[string]any{"cpu_limit": "2"}))
		})

		It("returns undefined before compile", func() {
			result, err := engine.EvaluatePolicy(ctx, "anything", map[string]any{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Defined).To(BeFalse())
		})

		It("evaluates namespaced package by ID", func() {
			err := engine.Compile(ctx, []opa.PolicyModule{
				{ID: "ns", RegoCode: "package policies.my_policy\nmain = {\"rejected\": false}"},
			})
			Expect(err).NotTo(HaveOccurred())

			result, err := engine.EvaluatePolicy(ctx, "ns", map[string]any{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Defined).To(BeTrue())
		})

		It("handles concurrent evaluation during compile", func() {
			// Compile initial policies
			err := engine.Compile(ctx, []opa.PolicyModule{
				{ID: "conc", RegoCode: "package concurrent\nmain = {\"rejected\": false}"},
			})
			Expect(err).NotTo(HaveOccurred())

			var wg sync.WaitGroup
			const numEvals = 50

			// Run evaluations concurrently
			for i := 0; i < numEvals; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer GinkgoRecover()

					result, err := engine.EvaluatePolicy(ctx, "conc", map[string]any{})
					Expect(err).NotTo(HaveOccurred())
					// Result might be defined or undefined depending on timing with compile
					_ = result
				}()
			}

			// Compile concurrently
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer GinkgoRecover()

				err := engine.Compile(ctx, []opa.PolicyModule{
					{ID: "conc2", RegoCode: "package concurrent\nmain = {\"rejected\": true}"},
				})
				Expect(err).NotTo(HaveOccurred())
			}()

			wg.Wait()
		})
	})

	Describe("ValidateRego", func() {
		It("accepts valid code", func() {
			err := engine.ValidateRego(ctx, "package test\nmain = true")
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects invalid syntax", func() {
			err := engine.ValidateRego(ctx, "package test\n{invalid")
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, opa.ErrInvalidRego)).To(BeTrue())
		})

		It("rejects empty code", func() {
			err := engine.ValidateRego(ctx, "")
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, opa.ErrInvalidRego)).To(BeTrue())
		})

		It("rejects code without a main rule", func() {
			err := engine.ValidateRego(ctx, "package test\ndefault allow = true")
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, opa.ErrMissingMainRule)).To(BeTrue())
		})
	})
})
