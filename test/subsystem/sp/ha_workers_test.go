//go:build subsystem

package subsystem_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/dcm-project/control-plane/api/sp/v1alpha1/resource_manager"
	"github.com/dcm-project/control-plane/internal/sp/messaging"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// agentDeleteCounter listens on an agent request subject for delete CloudEvents.
// Core NATS subscriptions receive JetStream publishes on the same subject when
// interest exists at publish time (see NATS JetStream delivery semantics).
type agentDeleteCounter struct {
	mu    sync.Mutex
	count int
	nc    *nats.Conn
	sub   *nats.Subscription
}

func newAgentDeleteCounter(topicName string) *agentDeleteCounter {
	nc, err := nats.Connect(natsURL())
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	counter := &agentDeleteCounter{nc: nc}
	sub, err := nc.Subscribe(topicName, func(msg *nats.Msg) {
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			return
		}
		if envelope.Type != messaging.CETypeDeleteRequest {
			return
		}
		counter.mu.Lock()
		counter.count++
		counter.mu.Unlock()
	})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	counter.sub = sub
	ExpectWithOffset(1, nc.Flush()).To(Succeed())
	return counter
}

func (c *agentDeleteCounter) get() int {
	c.mu.Lock()
	n := c.count
	c.mu.Unlock()
	return n
}

func (c *agentDeleteCounter) close() {
	if c.sub != nil {
		_ = c.sub.Unsubscribe()
	}
	if c.nc != nil {
		c.nc.Close()
	}
}

// Requires docker-compose.yaml control-plane-2 (two replicas sharing Postgres).
var _ = Describe("HA background workers", func() {
	BeforeEach(func() {
		ensureControlPlane2Replica()
		requireTwoControlPlaneReplicas()
	})

	AfterEach(func() {
		stopControlPlane2Replica()
	})

	It("runs deferred cleanup only once with two control-plane replicas", func() {
		agentName, instanceID := createInstanceViaAgent()
		topicName := "dcm.agent." + agentName

		counter := newAgentDeleteCounter(topicName)
		defer counter.close()

		deferred := true
		deleteResp, err := rmApiClient.DeleteInstanceWithResponse(context.Background(), instanceID, &resource_manager.DeleteInstanceParams{
			Deferred: &deferred,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(deleteResp.StatusCode()).To(Equal(http.StatusNoContent))

		// Deferred delete publishes from the API handler immediately; wait for
		// that to land, then measure only the next cleanup-scheduler tick.
		// Without an ack the scheduler retries every CLEANUP_INTERVAL, so a
		// long Eventually(==1) on the raw count will never match once retries
		// stack up.
		Eventually(counter.get, 3*time.Second, 100*time.Millisecond).Should(BeNumerically(">=", 1))
		baseline := counter.get()

		Eventually(func() int {
			return counter.get() - baseline
		}, 8*time.Second, 100*time.Millisecond).Should(Equal(1),
			"cleanup schedulers should publish exactly once per tick across replicas")

		totalAfterCleanup := counter.get()

		acknowledgeDeletion(instanceID, agentName)

		Consistently(counter.get, 12*time.Second, 2*time.Second).Should(Equal(totalAfterCleanup))

		// Deferred delete keeps a tombstone: ack marks deletion_status DELETED,
		// it does not hard-delete the row (unlike a non-deferred delete path).
		showDeleted := true
		Eventually(func(g Gomega) string {
			getResp, err := rmApiClient.GetInstanceWithResponse(context.Background(), instanceID, &resource_manager.GetInstanceParams{
				ShowDeleted: &showDeleted,
			})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			g.Expect(getResp.JSON200.DeletionStatus).NotTo(BeNil())
			return string(*getResp.JSON200.DeletionStatus)
		}, 30*time.Second, time.Second).Should(Equal("DELETED"))

		Eventually(func(g Gomega) int {
			getResp, err := rmApiClient.GetInstanceWithResponse(context.Background(), instanceID, nil)
			g.Expect(err).NotTo(HaveOccurred())
			return getResp.StatusCode()
		}, 10*time.Second, 500*time.Millisecond).Should(Equal(http.StatusNotFound))
	})
})
