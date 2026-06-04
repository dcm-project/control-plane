package provider_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestProviderService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Provider Service Suite")
}
