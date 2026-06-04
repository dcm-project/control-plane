package resource_manager_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestResourceManagerHandlers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Resource Manager Handlers Suite")
}
