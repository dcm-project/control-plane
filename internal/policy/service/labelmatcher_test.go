package service

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Test suite is registered in other test files - don't register again

var _ = Describe("MatchesLabelSelector", func() {
	It("matches when selector is empty", func() {
		policySelector := map[string]string{}
		requestLabels := map[string]string{
			"env": "prod",
			"app": "web",
		}

		Expect(MatchesLabelSelector(policySelector, requestLabels)).To(BeTrue())
	})

	It("matches when all selector labels are present and equal", func() {
		policySelector := map[string]string{
			"env": "prod",
			"app": "web",
		}
		requestLabels := map[string]string{
			"env": "prod",
			"app": "web",
		}

		Expect(MatchesLabelSelector(policySelector, requestLabels)).To(BeTrue())
	})

	It("matches when request has extra labels", func() {
		policySelector := map[string]string{
			"env": "prod",
		}
		requestLabels := map[string]string{
			"env":  "prod",
			"app":  "web",
			"team": "backend",
		}

		Expect(MatchesLabelSelector(policySelector, requestLabels)).To(BeTrue())
	})

	It("does not match when selector label is missing from request", func() {
		policySelector := map[string]string{
			"env": "prod",
			"app": "web",
		}
		requestLabels := map[string]string{
			"env": "prod",
		}

		Expect(MatchesLabelSelector(policySelector, requestLabels)).To(BeFalse())
	})

	It("does not match when selector label value differs", func() {
		policySelector := map[string]string{
			"env": "prod",
		}
		requestLabels := map[string]string{
			"env": "dev",
		}

		Expect(MatchesLabelSelector(policySelector, requestLabels)).To(BeFalse())
	})

	It("matches empty selector with empty request labels", func() {
		policySelector := map[string]string{}
		requestLabels := map[string]string{}

		Expect(MatchesLabelSelector(policySelector, requestLabels)).To(BeTrue())
	})

	It("does not match when request labels are empty but selector is not", func() {
		policySelector := map[string]string{
			"env": "prod",
		}
		requestLabels := map[string]string{}

		Expect(MatchesLabelSelector(policySelector, requestLabels)).To(BeFalse())
	})
})
