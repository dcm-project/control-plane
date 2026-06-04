package consumer_test

import (
	"os"
	"testing"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats-server/v2/test"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var testNATSServer *natsserver.Server

func TestConsumer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Consumer Suite")
}

var _ = BeforeSuite(func() {
	storeDir, err := os.MkdirTemp("", "nats-js-test-*")
	Expect(err).NotTo(HaveOccurred())

	opts := test.DefaultTestOptions
	opts.Port = -1
	opts.JetStream = true
	opts.StoreDir = storeDir
	testNATSServer = test.RunServer(&opts)
})

var _ = AfterSuite(func() {
	if testNATSServer != nil {
		testNATSServer.Shutdown()
	}
})
