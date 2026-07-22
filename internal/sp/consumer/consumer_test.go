package consumer_test

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2/event"
	"github.com/dcm-project/control-plane/internal/sp/consumer"
	"github.com/dcm-project/control-plane/internal/sp/store"
	"github.com/dcm-project/control-plane/internal/sp/store/model"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type statusConsumerTestBG struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// startStatusConsumerTestContext keeps context.WithCancel out of BeforeEach (fatcontext).
func startStatusConsumerTestContext(bg *statusConsumerTestBG) {
	bg.ctx, bg.cancel = context.WithCancel(context.Background())
}

var _ = Describe("StatusConsumer", func() {
	var (
		db        *gorm.DB
		dataStore store.Store
		nc        *nats.Conn
		js        jetstream.JetStream
		sc        *consumer.StatusConsumer
		bg        statusConsumerTestBG
		natsURL   string
		streamID  string
	)

	BeforeEach(func() {
		var err error

		// Setup in-memory database
		db, err = gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		sqlDB, err := db.DB()
		Expect(err).NotTo(HaveOccurred())
		sqlDB.SetMaxOpenConns(1)
		Expect(db.AutoMigrate(&model.ServiceTypeInstance{})).To(Succeed())
		dataStore = store.NewStore(db)

		// Use the NATS test server URL from suite_test.go
		natsURL = testNATSServer.ClientURL()

		// Connect a publisher client with JetStream
		nc, err = nats.Connect(natsURL)
		Expect(err).NotTo(HaveOccurred())
		js, err = jetstream.New(nc)
		Expect(err).NotTo(HaveOccurred())

		// Use unique stream/consumer names per test to avoid conflicts
		streamID = uuid.New().String()[:8]

		// Create and start the consumer
		sc, err = consumer.New(natsURL, "dcm.*", dataStore,
			consumer.SetStreamName("test-stream-"+streamID),
			consumer.SetConsumerName("test-consumer-"+streamID),
		)
		Expect(err).NotTo(HaveOccurred())

		startStatusConsumerTestContext(&bg)
		Expect(sc.Start(bg.ctx)).To(Succeed())
	})

	AfterEach(func() {
		sc.Stop()
		_ = js.DeleteStream(context.Background(), "test-stream-"+streamID)
		nc.Close()
		bg.cancel()
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	publishStatusEvent := func(providerName, serviceType, instanceID, status, message string) {
		event := cloudevents.New()
		event.SetID(uuid.New().String())
		event.SetSource(fmt.Sprintf("dcm/providers/%s", providerName))
		event.SetType(fmt.Sprintf("dcm.status.%s", serviceType))
		event.SetTime(time.Now())

		payload := consumer.StatusEvent{
			Id:        instanceID,
			Status:    status,
			Message:   message,
			Timestamp: time.Now(),
		}
		Expect(event.SetData(cloudevents.ApplicationJSON, payload)).To(Succeed())

		data, err := json.Marshal(event)
		Expect(err).NotTo(HaveOccurred())

		subject := fmt.Sprintf("dcm.%s", serviceType)
		_, err = js.Publish(bg.ctx, subject, data)
		Expect(err).NotTo(HaveOccurred())
	}

	createInstance := func(instanceID string) {
		instance := model.ServiceTypeInstance{
			ID:           instanceID,
			ProviderName: "test-provider",
			Status:       "PROVISIONING",
			InstanceName: "test-instance",
			Spec:         map[string]any{"cpu": "2"},
		}
		_, err := dataStore.ServiceTypeInstance().Create(bg.ctx, instance)
		Expect(err).NotTo(HaveOccurred())
	}

	It("updates instance status on valid status event", func() {
		instanceID := uuid.New().String()
		createInstance(instanceID)

		publishStatusEvent("kubevirt-sp", "vm", instanceID, "RUNNING", "VM is running")

		Eventually(func() string {
			var inst model.ServiceTypeInstance
			db.Where("id = ?", instanceID).First(&inst)
			return inst.Status
		}, 2*time.Second, 100*time.Millisecond).Should(Equal("RUNNING"))
	})

	It("updates status message along with status", func() {
		instanceID := uuid.New().String()
		createInstance(instanceID)

		publishStatusEvent("kubevirt-sp", "vm", instanceID, "FAILED", "VM crashed unexpectedly")

		Eventually(func() string {
			var inst model.ServiceTypeInstance
			db.Where("id = ?", instanceID).First(&inst)
			return inst.StatusMessage
		}, 2*time.Second, 100*time.Millisecond).Should(Equal("VM crashed unexpectedly"))
	})

	It("handles events for non-existent instances gracefully", func() {
		publishStatusEvent("kubevirt-sp", "vm", "non-existent-id", "RUNNING", "VM is running")

		// Give it time to process - no panic expected
		time.Sleep(200 * time.Millisecond)
	})

	It("discards malformed CloudEvent messages", func() {
		_, err := js.Publish(bg.ctx, "dcm.vm", []byte("not-valid-json"))
		Expect(err).NotTo(HaveOccurred())

		// Give it time to process - no panic expected
		time.Sleep(200 * time.Millisecond)
	})

	It("handles multiple sequential status updates", func() {
		instanceID := uuid.New().String()
		createInstance(instanceID)

		publishStatusEvent("kubevirt-sp", "vm", instanceID, "PROVISIONING", "Starting VM")

		Eventually(func() string {
			var inst model.ServiceTypeInstance
			db.Where("id = ?", instanceID).First(&inst)
			return inst.Status
		}, 2*time.Second, 100*time.Millisecond).Should(Equal("PROVISIONING"))

		time.Sleep(200 * time.Millisecond)

		publishStatusEvent("kubevirt-sp", "vm", instanceID, "RUNNING", "VM is running")

		Eventually(func() string {
			var inst model.ServiceTypeInstance
			db.Where("id = ?", instanceID).First(&inst)
			return inst.Status
		}, 2*time.Second, 100*time.Millisecond).Should(Equal("RUNNING"))
	})

	It("Check succeeds while connected", func() {
		Expect(sc.Check(bg.ctx)).To(Succeed())
	})
})
