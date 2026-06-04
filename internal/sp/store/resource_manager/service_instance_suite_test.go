package store_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestResourceManagerStore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Resource Manager Store Suite")
}
