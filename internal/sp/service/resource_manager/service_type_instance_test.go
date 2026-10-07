package resource_manager_test

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/dcm-project/control-plane/api/sp/v1alpha1/resource_manager"
	agentstore "github.com/dcm-project/control-plane/internal/agent/store/agent"
	agentmodel "github.com/dcm-project/control-plane/internal/agent/store/model"
	"github.com/dcm-project/control-plane/internal/sp/messaging"
	"github.com/dcm-project/control-plane/internal/sp/service"
	rmsvc "github.com/dcm-project/control-plane/internal/sp/service/resource_manager"
	"github.com/dcm-project/control-plane/internal/sp/store"
	"github.com/dcm-project/control-plane/internal/sp/store/model"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// stubJetStream acknowledges every publish so tests can exercise the
// agent-routed CreateInstance/ReassignAgent paths without a real NATS server.
type stubJetStream struct {
	jetstream.JetStream
	subjects []string
}

func (s *stubJetStream) Publish(_ context.Context, subject string, _ []byte, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	s.subjects = append(s.subjects, subject)
	return &jetstream.PubAck{}, nil
}

type agentLookupResult struct {
	agent *agentmodel.Agent
	err   error
}

type sequencedAgentStore struct {
	agentstore.Agent
	results  []agentLookupResult
	calls    int
	onLookup func(call int)
}

func (s *sequencedAgentStore) GetByName(_ context.Context, _ string) (*agentmodel.Agent, error) {
	call := s.calls
	s.calls++
	if s.onLookup != nil {
		s.onLookup(call)
	}
	if call >= len(s.results) {
		return nil, errors.New("unexpected agent lookup")
	}
	return s.results[call].agent, s.results[call].err
}

func ptrString(s string) *string { return &s }

type batchHealthLookup interface {
	GetHealthByNames(ctx context.Context, names []string) (map[string]agentmodel.AgentHealthStatus, error)
}

type batchHealthAgentStore struct {
	agentstore.Agent
	calls int
	names [][]string
}

func (s *batchHealthAgentStore) GetHealthByNames(ctx context.Context, names []string) (map[string]agentmodel.AgentHealthStatus, error) {
	s.calls++
	s.names = append(s.names, append([]string(nil), names...))
	lookup, ok := s.Agent.(batchHealthLookup)
	if !ok {
		return nil, errors.New("underlying agent store does not support batch health lookup")
	}
	return lookup.GetHealthByNames(ctx, names)
}

func responseObject(response any) map[string]any {
	body, err := json.Marshal(response)
	Expect(err).NotTo(HaveOccurred())
	var object map[string]any
	Expect(json.Unmarshal(body, &object)).To(Succeed())
	return object
}

var _ = Describe("InstanceService", func() {
	var (
		db              *gorm.DB
		dataStore       store.Store
		instanceService *rmsvc.InstanceService
		ctx             context.Context
		agentStore      agentstore.Agent
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&agentmodel.Agent{}, &model.ServiceTypeInstance{})).To(Succeed())
		Expect(db.Create(&agentmodel.Agent{ID: uuid.New().String(), Name: "test-agent", TopicName: "dcm.agent.test-agent", HealthStatus: agentmodel.AgentHealthStatusReady, ServiceTypes: []string{"vm", "container"}}).Error).NotTo(HaveOccurred())

		dataStore = store.NewStore(db)
		pub := messaging.NewPublisher(&stubJetStream{})
		agentStore = agentstore.NewAgent(db)
		instanceService = rmsvc.NewInstanceService(dataStore, pub, agentStore)
		ctx = context.Background()
	})

	AfterEach(func() {
		_ = dataStore.Close()
	})

	Describe("CreateInstance (agent-routed provisioning)", func() {
		It("creates instance with pending status via agent NATS", func() {
			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 2, "memory": "4GB", "service_type": "vm"},
			}

			result, err := instanceService.CreateInstance(ctx, req, nil, "test-agent")

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Id).NotTo(BeNil())

			var stored model.ServiceTypeInstance
			Expect(db.First(&stored, "id = ?", *result.Id).Error).NotTo(HaveOccurred())
			Expect(stored.Status).To(Equal("pending"))
		})

		It("publishes create to the current agent topic after it changes", func() {
			oldAgent := &agentmodel.Agent{
				Name:         "test-agent",
				TopicName:    "dcm.agent.old-topic",
				HealthStatus: agentmodel.AgentHealthStatusReady,
				ServiceTypes: []string{"vm"},
			}
			currentAgent := *oldAgent
			currentAgent.TopicName = "dcm.agent.current-topic"
			sequencedStore := &sequencedAgentStore{results: []agentLookupResult{
				{agent: oldAgent},
				{agent: &currentAgent},
			}}
			jetStream := &stubJetStream{}
			instanceService = rmsvc.NewInstanceService(dataStore, messaging.NewPublisher(jetStream), sequencedStore)
			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 2, "service_type": "vm"},
			}

			_, err := instanceService.CreateInstance(ctx, req, nil, "test-agent")

			Expect(err).NotTo(HaveOccurred())
			Expect(sequencedStore.calls).To(Equal(2))
			Expect(jetStream.subjects).To(Equal([]string{"dcm.agent.current-topic"}))
		})

		It("rolls back the created instance when resolving the current agent topic fails", func() {
			instanceID := uuid.New().String()
			validatedAgent := &agentmodel.Agent{
				Name:         "test-agent",
				TopicName:    "dcm.agent.test-agent",
				HealthStatus: agentmodel.AgentHealthStatusReady,
				ServiceTypes: []string{"vm"},
			}
			instanceExistedDuringResolution := false
			sequencedStore := &sequencedAgentStore{
				results: []agentLookupResult{
					{agent: validatedAgent},
					{err: agentstore.ErrAgentNotFound},
				},
				onLookup: func(call int) {
					if call != 1 {
						return
					}
					var stored model.ServiceTypeInstance
					instanceExistedDuringResolution = db.First(&stored, "id = ?", instanceID).Error == nil
				},
			}
			jetStream := &stubJetStream{}
			instanceService = rmsvc.NewInstanceService(dataStore, messaging.NewPublisher(jetStream), sequencedStore)
			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 2, "service_type": "vm"},
			}

			_, err := instanceService.CreateInstance(ctx, req, &instanceID, "test-agent")

			var svcErr *service.ServiceError
			Expect(errors.As(err, &svcErr)).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeProvisioningError))
			Expect(sequencedStore.calls).To(Equal(2))
			Expect(instanceExistedDuringResolution).To(BeTrue())
			var count int64
			Expect(db.Model(&model.ServiceTypeInstance{}).Where("id = ?", instanceID).Count(&count).Error).To(Succeed())
			Expect(count).To(BeZero())
			Expect(jetStream.subjects).To(BeEmpty())
		})

		It("sets service_type from spec", func() {
			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 2, "service_type": "vm"},
			}

			result, err := instanceService.CreateInstance(ctx, req, nil, "test-agent")

			Expect(err).NotTo(HaveOccurred())
			var dbInstance model.ServiceTypeInstance
			Expect(db.Where("id = ?", *result.Id).First(&dbInstance).Error).NotTo(HaveOccurred())
			Expect(dbInstance.ServiceType).To(Equal("vm"))
		})

		It("creates instance with specified ID", func() {
			specifiedID := uuid.New().String()
			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 1, "service_type": "vm"},
			}

			result, err := instanceService.CreateInstance(ctx, req, &specifiedID, "test-agent")

			Expect(err).NotTo(HaveOccurred())
			Expect(*result.Id).To(Equal(specifiedID))
		})

		It("returns conflict error for duplicate ID", func() {
			specifiedID := uuid.New().String()
			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 1, "service_type": "vm"},
			}

			_, err := instanceService.CreateInstance(ctx, req, &specifiedID, "test-agent")
			Expect(err).NotTo(HaveOccurred())

			_, err = instanceService.CreateInstance(ctx, req, &specifiedID, "test-agent")

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			errors.As(err, &svcErr)
			Expect(svcErr.Code).To(Equal(service.ErrCodeConflict))
		})

		It("returns validation error when spec is missing service_type", func() {
			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 2},
			}

			_, err := instanceService.CreateInstance(ctx, req, nil, "test-agent")

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			errors.As(err, &svcErr)
			Expect(svcErr.Code).To(Equal(service.ErrCodeValidation))
			Expect(svcErr.Message).To(ContainSubstring("spec.service_type is required"))
		})

		It("returns validation error when spec.service_type is not a string", func() {
			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 2, "service_type": 42},
			}

			_, err := instanceService.CreateInstance(ctx, req, nil, "test-agent")

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			errors.As(err, &svcErr)
			Expect(svcErr.Code).To(Equal(service.ErrCodeValidation))
		})

		It("returns validation error when spec.service_type is empty", func() {
			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 2, "service_type": ""},
			}

			_, err := instanceService.CreateInstance(ctx, req, nil, "test-agent")

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			errors.As(err, &svcErr)
			Expect(svcErr.Code).To(Equal(service.ErrCodeValidation))
			Expect(svcErr.Message).To(ContainSubstring("must not be empty"))
		})

		It("returns validation error when spec.service_type is whitespace only", func() {
			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 2, "service_type": " "},
			}

			_, err := instanceService.CreateInstance(ctx, req, nil, "test-agent")

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			errors.As(err, &svcErr)
			Expect(svcErr.Code).To(Equal(service.ErrCodeValidation))
			Expect(svcErr.Message).To(ContainSubstring("must not be empty"))
		})

		It("returns validation error when agentName is empty instead of creating an orphan pending row", func() {
			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 2, "service_type": "vm"},
			}

			_, err := instanceService.CreateInstance(ctx, req, nil, "")

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			errors.As(err, &svcErr)
			Expect(svcErr.Code).To(Equal(service.ErrCodeValidation))
			Expect(svcErr.Message).To(ContainSubstring("agent_name"))

			var count int64
			Expect(db.Model(&model.ServiceTypeInstance{}).Count(&count).Error).NotTo(HaveOccurred())
			Expect(count).To(BeZero(), "no instance row should have been created")
		})

		It("returns validation error when agentName is whitespace only", func() {
			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 2, "service_type": "vm"},
			}

			_, err := instanceService.CreateInstance(ctx, req, nil, "   ")

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			errors.As(err, &svcErr)
			Expect(svcErr.Code).To(Equal(service.ErrCodeValidation))
		})
	})

	Describe("GetInstance", func() {
		It("returns an instance", func() {
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "pending",
				InstanceName: "get-inst",
				Spec:         map[string]any{"cpu": 2},
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

			result, err := instanceService.GetInstance(ctx, inst.ID, false)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(*result.Id).To(Equal(inst.ID))
		})

		It("returns not found error for non-existent instance", func() {
			_, err := instanceService.GetInstance(ctx, uuid.New().String(), false)

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			errors.As(err, &svcErr)
			Expect(svcErr.Code).To(Equal(service.ErrCodeNotFound))
		})
	})

	Describe("GetOutputSpec", func() {
		It("returns persisted output_spec", func() {
			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 2, "service_type": "vm"},
			}
			created, err := instanceService.CreateInstance(ctx, req, nil, "test-agent")
			Expect(err).NotTo(HaveOccurred())

			outputSpec := map[string]any{"connection_string": "postgres://db:5432/orders"}
			err = dataStore.ServiceTypeInstance().UpdateStatus(ctx, *created.Id, "RUNNING", "ready", outputSpec)
			Expect(err).NotTo(HaveOccurred())

			got, err := instanceService.GetOutputSpec(ctx, *created.Id)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(outputSpec))
		})

		It("returns not found error for non-existent instance", func() {
			_, err := instanceService.GetOutputSpec(ctx, uuid.New().String())

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			errors.As(err, &svcErr)
			Expect(svcErr.Code).To(Equal(service.ErrCodeNotFound))
		})
	})

	Describe("ListInstances", func() {
		It("returns empty list when no instances exist", func() {
			result, err := instanceService.ListInstances(ctx, nil, nil, false, nil, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(*result.Instances).To(BeEmpty())
		})

		It("returns all instances", func() {
			for i := 0; i < 3; i++ {
				inst := model.ServiceTypeInstance{
					ID:           uuid.New().String(),
					ServiceType:  "vm",
					Status:       "pending",
					InstanceName: uuid.New().String(),
					Spec:         map[string]any{"cpu": i + 1},
				}
				Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			}

			result, err := instanceService.ListInstances(ctx, nil, nil, false, nil, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(*result.Instances).To(HaveLen(3))
		})

		It("filters instances by service type", func() {
			for i := 0; i < 2; i++ {
				inst := model.ServiceTypeInstance{
					ID:           uuid.New().String(),
					ServiceType:  "vm",
					Status:       "pending",
					InstanceName: uuid.New().String(),
					Spec:         map[string]any{"cpu": i + 1},
				}
				Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			}
			for i := 0; i < 3; i++ {
				inst := model.ServiceTypeInstance{
					ID:           uuid.New().String(),
					ServiceType:  "container",
					Status:       "pending",
					InstanceName: uuid.New().String(),
					Spec:         map[string]any{"image": "nginx"},
				}
				Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			}

			vmType := "vm"
			result, err := instanceService.ListInstances(ctx, &vmType, nil, false, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(*result.Instances).To(HaveLen(2))

			containerType := "container"
			result, err = instanceService.ListInstances(ctx, &containerType, nil, false, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(*result.Instances).To(HaveLen(3))
		})

		It("filters instances by agent name", func() {
			agentA, agentB := "agent-a", "agent-b"
			for i := 0; i < 2; i++ {
				inst := model.ServiceTypeInstance{
					ID:           uuid.New().String(),
					ServiceType:  "vm",
					Status:       "pending",
					InstanceName: uuid.New().String(),
					Spec:         map[string]any{"cpu": i + 1},
					AgentName:    &agentA,
				}
				Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			}
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "pending",
				InstanceName: uuid.New().String(),
				Spec:         map[string]any{"cpu": 3},
				AgentName:    &agentB,
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

			result, err := instanceService.ListInstances(ctx, nil, &agentA, false, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(*result.Instances).To(HaveLen(2))

			result, err = instanceService.ListInstances(ctx, nil, &agentB, false, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(*result.Instances).To(HaveLen(1))
		})
	})

	Describe("health-enriched responses", func() {
		It("Given a page with different agents and repeated ownership, when listed, then each page item has its current health and STI status is unchanged", func() {
			agents := []agentmodel.Agent{
				{ID: uuid.New().String(), Name: "health-unavailable", TopicName: "dcm.agent.health-unavailable", HealthStatus: agentmodel.AgentHealthStatusUnavailable, ServiceTypes: []string{"vm"}},
				{ID: uuid.New().String(), Name: "health-congested", TopicName: "dcm.agent.health-congested", HealthStatus: agentmodel.AgentHealthStatusCongested, ServiceTypes: []string{"vm"}},
				{ID: uuid.New().String(), Name: "health-outside-page", TopicName: "dcm.agent.health-outside-page", HealthStatus: agentmodel.AgentHealthStatusReady, ServiceTypes: []string{"vm"}},
			}
			for i := range agents {
				Expect(db.Create(&agents[i]).Error).NotTo(HaveOccurred())
			}

			baseTime := time.Now().Add(-4 * time.Minute)
			instances := []model.ServiceTypeInstance{
				{ID: "health-page-1", ServiceType: "vm", Status: "running", InstanceName: "running-unavailable", Spec: map[string]any{"cpu": 1}, AgentName: ptrString("health-unavailable"), CreateTime: baseTime},
				{ID: "health-page-2", ServiceType: "vm", Status: "pending", InstanceName: "pending-congested", Spec: map[string]any{"cpu": 2}, AgentName: ptrString("health-congested"), CreateTime: baseTime.Add(time.Minute)},
				{ID: "health-page-3", ServiceType: "vm", Status: "queued", InstanceName: "queued-unavailable", Spec: map[string]any{"cpu": 3}, AgentName: ptrString("health-unavailable"), CreateTime: baseTime.Add(2 * time.Minute)},
				{ID: "health-page-4", ServiceType: "vm", Status: "running", InstanceName: "outside-page-ready", Spec: map[string]any{"cpu": 4}, AgentName: ptrString("health-outside-page"), CreateTime: baseTime.Add(3 * time.Minute)},
			}
			for i := range instances {
				Expect(db.Create(&instances[i]).Error).NotTo(HaveOccurred())
			}

			spy := &batchHealthAgentStore{Agent: agentStore}
			instanceService = rmsvc.NewInstanceService(dataStore, messaging.NewPublisher(&stubJetStream{}), spy)
			pageSize := 3
			result, err := instanceService.ListInstances(ctx, nil, nil, false, &pageSize, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(*result.Instances).To(HaveLen(3))
			Expect(result.NextPageToken).NotTo(BeNil())
			Expect(spy.calls).To(Equal(1))
			Expect(spy.names).To(Equal([][]string{{"health-unavailable", "health-congested"}}))

			listJSON := responseObject(result)
			items := listJSON["instances"].([]any)
			byID := make(map[string]map[string]any, len(items))
			for _, item := range items {
				instance := item.(map[string]any)
				byID[instance["id"].(string)] = instance
			}
			Expect(byID["health-page-1"]["agent_health_status"]).To(Equal("unavailable"))
			Expect(byID["health-page-1"]["status"]).To(Equal("running"))
			Expect(byID["health-page-2"]["agent_health_status"]).To(Equal("congested"))
			Expect(byID["health-page-2"]["status"]).To(Equal("pending"))
			Expect(byID["health-page-3"]["agent_health_status"]).To(Equal("unavailable"))
			Expect(byID).NotTo(HaveKey("health-page-4"))
		})

		It("Given an associated agent whose health changes, when fetched, then the response reports current health without changing STI status", func() {
			inst := model.ServiceTypeInstance{ID: uuid.New().String(), ServiceType: "vm", Status: "running", InstanceName: "current-health", Spec: map[string]any{"cpu": 2}, AgentName: ptrString("test-agent")}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			Expect(db.Model(&agentmodel.Agent{}).Where("name = ?", "test-agent").Update("health_status", agentmodel.AgentHealthStatusCongested).Error).NotTo(HaveOccurred())

			result, err := instanceService.GetInstance(ctx, inst.ID, false)

			Expect(err).NotTo(HaveOccurred())
			got := responseObject(result)
			Expect(got["agent_health_status"]).To(Equal("congested"))
			Expect(got["status"]).To(Equal("running"))
		})

		It("Given a ready agent and accepted create request, when created, then the response includes ready separately from pending STI status", func() {
			request := &resource_manager.ServiceTypeInstance{Spec: map[string]any{"service_type": "vm", "cpu": 1}}

			result, err := instanceService.CreateInstance(ctx, request, nil, "test-agent")

			Expect(err).NotTo(HaveOccurred())
			got := responseObject(result)
			Expect(got["agent_health_status"]).To(Equal("ready"))
			Expect(got["status"]).To(Equal("pending"))
		})

		It("Given an STI without agent_name, when listed and fetched, then it remains available without agent health", func() {
			inst := model.ServiceTypeInstance{ID: uuid.New().String(), ServiceType: "vm", Status: "running", InstanceName: "no-agent", Spec: map[string]any{"cpu": 1}}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			spy := &batchHealthAgentStore{Agent: agentStore}
			instanceService = rmsvc.NewInstanceService(dataStore, messaging.NewPublisher(&stubJetStream{}), spy)

			list, listErr := instanceService.ListInstances(ctx, nil, nil, false, nil, nil)
			Expect(listErr).NotTo(HaveOccurred())
			listJSON := responseObject(list)
			items := listJSON["instances"].([]any)
			Expect(items).To(HaveLen(1))
			listed := items[0].(map[string]any)
			Expect(listed["agent_health_status"]).To(BeNil())
			Expect(spy.calls).To(BeZero())

			got, getErr := instanceService.GetInstance(ctx, inst.ID, false)
			Expect(getErr).NotTo(HaveOccurred())
			Expect(responseObject(got)["agent_health_status"]).To(BeNil())
		})

		It("Given agent health lookup database errors, when listed or fetched, then the service returns an internal error", func() {
			inst := model.ServiceTypeInstance{ID: uuid.New().String(), ServiceType: "vm", Status: "running", InstanceName: "lookup-error", Spec: map[string]any{"cpu": 1}, AgentName: ptrString("test-agent")}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			Expect(db.Migrator().DropTable(&agentmodel.Agent{})).To(Succeed())

			_, listErr := instanceService.ListInstances(ctx, nil, nil, false, nil, nil)
			_, getErr := instanceService.GetInstance(ctx, inst.ID, false)
			for _, err := range []error{listErr, getErr} {
				Expect(err).To(HaveOccurred())
				var svcErr *service.ServiceError
				Expect(errors.As(err, &svcErr)).To(BeTrue())
				Expect(svcErr.Code).To(Equal(service.ErrCodeInternal))
			}
		})
	})

	Describe("DeleteInstance (agent-routed)", func() {
		It("publishes delete event and marks deleting, awaiting agent acknowledgement, for non-deferred deletion", func() {
			agentName := "test-agent"
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "running",
				InstanceName: "del-inst",
				Spec:         map[string]any{"cpu": 2},
				AgentName:    &agentName,
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

			err := instanceService.DeleteInstance(ctx, inst.ID, false)
			Expect(err).NotTo(HaveOccurred())

			// The record must still exist as "deleting" until the agent's
			// deletion-acknowledged event confirms the physical resource is
			// gone, and be enrolled in retry tracking like a deferred delete.
			got, getErr := instanceService.GetInstance(ctx, inst.ID, true)
			Expect(getErr).NotTo(HaveOccurred())
			Expect(got.Status).NotTo(BeNil())
			Expect(*got.Status).To(Equal("deleting"))

			_, hiddenErr := instanceService.GetInstance(ctx, inst.ID, false)
			var svcErr *service.ServiceError
			Expect(hiddenErr).To(BeAssignableToTypeOf(svcErr))
			errors.As(hiddenErr, &svcErr)
			Expect(svcErr.Code).To(Equal(service.ErrCodeNotFound))
		})

		It("hard-deletes immediately when the instance has no agent", func() {
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "running",
				InstanceName: "del-inst-no-agent",
				Spec:         map[string]any{"cpu": 2},
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

			err := instanceService.DeleteInstance(ctx, inst.ID, false)
			Expect(err).NotTo(HaveOccurred())

			_, getErr := instanceService.GetInstance(ctx, inst.ID, false)
			var svcErr *service.ServiceError
			Expect(getErr).To(BeAssignableToTypeOf(svcErr))
			errors.As(getErr, &svcErr)
			Expect(svcErr.Code).To(Equal(service.ErrCodeNotFound))
		})

		It("hard-deletes immediately when the assigned agent no longer exists (C)", func() {
			// Without this, publishDeleteToAgent's old behavior (silently
			// treating ErrAgentNotFound as a successful publish) would leave
			// this instance stuck in "deleting" forever: no agent will ever
			// send a "deletion-acknowledged" for a nonexistent agent.
			gone := "nonexistent-agent"
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "running",
				InstanceName: "del-inst-gone-agent",
				Spec:         map[string]any{"cpu": 2},
				AgentName:    &gone,
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

			err := instanceService.DeleteInstance(ctx, inst.ID, false)
			Expect(err).NotTo(HaveOccurred())

			_, getErr := instanceService.GetInstance(ctx, inst.ID, true)
			var svcErr *service.ServiceError
			Expect(getErr).To(BeAssignableToTypeOf(svcErr))
			errors.As(getErr, &svcErr)
			Expect(svcErr.Code).To(Equal(service.ErrCodeNotFound))
		})

		It("returns not found error for non-existent instance", func() {
			err := instanceService.DeleteInstance(ctx, uuid.New().String(), false)

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			errors.As(err, &svcErr)
			Expect(svcErr.Code).To(Equal(service.ErrCodeNotFound))
		})

		It("defers deletion without contacting provider", func() {
			agentName := "test-agent"
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "running",
				InstanceName: "defer-del-inst",
				Spec:         map[string]any{"cpu": 2},
				AgentName:    &agentName,
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

			err := instanceService.DeleteInstance(ctx, inst.ID, true)
			Expect(err).NotTo(HaveOccurred())

			result, err := instanceService.ListInstances(ctx, nil, nil, false, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(*result.Instances).To(BeEmpty())

			result, err = instanceService.ListInstances(ctx, nil, nil, true, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(*result.Instances).To(HaveLen(1))
			Expect(string(*(*result.Instances)[0].DeletionStatus)).To(Equal("SCHEDULED"))
		})

		It("surfaces deletion_status DELETED on the API struct after cleanup completes", func() {
			agentName := "test-agent"
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "running",
				InstanceName: "deleted-api-inst",
				Spec:         map[string]any{"cpu": 2},
				AgentName:    &agentName,
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			Expect(dataStore.ServiceTypeInstance().MarkForDeletion(ctx, inst.ID)).To(Succeed())
			Expect(dataStore.ServiceTypeInstance().MarkDeletionComplete(ctx, inst.ID)).To(Succeed())

			got, err := instanceService.GetInstance(ctx, inst.ID, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.DeletionStatus).NotTo(BeNil())
			Expect(string(*got.DeletionStatus)).To(Equal("DELETED"))

			result, err := instanceService.ListInstances(ctx, nil, nil, true, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(*result.Instances).To(HaveLen(1))
			Expect(string(*(*result.Instances)[0].DeletionStatus)).To(Equal("DELETED"))
		})

		It("surfaces deletion_status FAILED on the API struct after cleanup gives up", func() {
			agentName := "test-agent"
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "running",
				InstanceName: "failed-api-inst",
				Spec:         map[string]any{"cpu": 2},
				AgentName:    &agentName,
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			Expect(dataStore.ServiceTypeInstance().MarkForDeletion(ctx, inst.ID)).To(Succeed())
			Expect(dataStore.ServiceTypeInstance().MarkDeletionFailed(ctx, inst.ID)).To(Succeed())

			got, err := instanceService.GetInstance(ctx, inst.ID, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.DeletionStatus).NotTo(BeNil())
			Expect(string(*got.DeletionStatus)).To(Equal("FAILED"))
		})
	})

	Describe("InstanceService agent fields", func() {
		It("stores agent_name on instance record and surfaces it on the API struct (F19)", func() {
			agentName := "test-agent"
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "pending",
				InstanceName: "agent-inst",
				Spec:         map[string]any{"cpu": 2},
				AgentName:    &agentName,
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

			var stored model.ServiceTypeInstance
			Expect(db.First(&stored, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
			Expect(stored.AgentName).NotTo(BeNil())
			Expect(*stored.AgentName).To(Equal("test-agent"))

			// ModelToAPI must populate AgentName on the returned API struct
			// too, not just the DB row.
			got, err := instanceService.GetInstance(ctx, inst.ID, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.AgentName).NotTo(BeNil())
			Expect(*got.AgentName).To(Equal("test-agent"))
		})
	})

	Describe("Agent validation", func() {
		It("rejects creation when agent is not found", func() {
			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 2, "service_type": "vm"},
			}

			_, err := instanceService.CreateInstance(ctx, req, nil, "nonexistent-agent")

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(errors.As(err, &svcErr)).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeNotFound))
		})

		It("rejects creation when agent is unavailable", func() {
			Expect(db.Create(&agentmodel.Agent{
				ID:           uuid.New().String(),
				Name:         "unavailable-agent",
				TopicName:    "dcm.agent.unavailable-agent",
				HealthStatus: agentmodel.AgentHealthStatusUnavailable,
				ServiceTypes: []string{"vm"},
			}).Error).NotTo(HaveOccurred())

			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 2, "service_type": "vm"},
			}

			_, err := instanceService.CreateInstance(ctx, req, nil, "unavailable-agent")

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(errors.As(err, &svcErr)).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeUnavailable))
		})

		It("rejects creation when agent is congested", func() {
			Expect(db.Create(&agentmodel.Agent{
				ID:           uuid.New().String(),
				Name:         "congested-agent",
				TopicName:    "dcm.agent.congested-agent",
				HealthStatus: agentmodel.AgentHealthStatusCongested,
				ServiceTypes: []string{"vm"},
			}).Error).NotTo(HaveOccurred())

			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 2, "service_type": "vm"},
			}

			_, err := instanceService.CreateInstance(ctx, req, nil, "congested-agent")

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(errors.As(err, &svcErr)).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeUnavailable))
		})

		It("rejects creation when agent does not serve the requested service type", func() {
			Expect(db.Create(&agentmodel.Agent{
				ID:           uuid.New().String(),
				Name:         "container-only-agent",
				TopicName:    "dcm.agent.container-only-agent",
				HealthStatus: agentmodel.AgentHealthStatusReady,
				ServiceTypes: []string{"container"},
			}).Error).NotTo(HaveOccurred())

			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 2, "service_type": "vm"},
			}

			_, err := instanceService.CreateInstance(ctx, req, nil, "container-only-agent")

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(errors.As(err, &svcErr)).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeValidation))
			Expect(svcErr.Message).To(ContainSubstring("does not serve service type"))
		})

		It("accepts creation when agent is ready and serves the service type", func() {
			req := &resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 2, "service_type": "vm"},
			}

			result, err := instanceService.CreateInstance(ctx, req, nil, "test-agent")

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
		})
	})

	Describe("ReassignAgent", func() {
		BeforeEach(func() {
			Expect(db.Create(&agentmodel.Agent{ID: uuid.New().String(), Name: "fallback-agent", TopicName: "dcm.agent.fallback-agent", HealthStatus: agentmodel.AgentHealthStatusReady, ServiceTypes: []string{"vm"}}).Error).NotTo(HaveOccurred())
		})

		It("reassigns when expectedCurrentAgent matches the instance's current agent", func() {
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "pending",
				InstanceName: "reassign-cas-match",
				Spec:         map[string]any{"cpu": 2},
				AgentName:    ptrString("test-agent"),
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

			err := instanceService.ReassignAgent(ctx, inst.ID, "fallback-agent", "test-agent")

			Expect(err).NotTo(HaveOccurred())
			var stored model.ServiceTypeInstance
			Expect(db.First(&stored, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
			Expect(*stored.AgentName).To(Equal("fallback-agent"))
		})

		It("rejects the reassignment when expectedCurrentAgent is stale (R2 T1: CAS parameter must be threaded end-to-end, not re-derived from a fresh read)", func() {
			// Proves expectedCurrentAgent actually reaches ReassignAndReset's
			// CAS rather than being silently overridden by a fresh Get()
			// inside ReassignAgent, which would defeat the whole guard: a
			// caller passing a stale/excluded agent it observed earlier
			// must be rejected here even though the DB's current agent_name
			// ("test-agent") looks otherwise eligible (pending, not deleted).
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "pending",
				InstanceName: "reassign-cas-stale",
				Spec:         map[string]any{"cpu": 2},
				AgentName:    ptrString("test-agent"),
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

			err := instanceService.ReassignAgent(ctx, inst.ID, "fallback-agent", "some-other-agent-the-caller-thinks-is-current")

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(errors.As(err, &svcErr)).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeConflict))

			var stored model.ServiceTypeInstance
			Expect(db.First(&stored, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
			Expect(*stored.AgentName).To(Equal("test-agent"))
		})

		It("returns validation error when agentName is empty", func() {
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "pending",
				InstanceName: "reassign-empty-agent",
				Spec:         map[string]any{"cpu": 2},
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

			err := instanceService.ReassignAgent(ctx, inst.ID, "", "")

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(errors.As(err, &svcErr)).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeValidation))
		})

		It("returns unavailable error when agent store is not configured", func() {
			// Regression test: ReassignAgent previously had no guard of its
			// own before calling validateAgent (unlike CreateInstance), and
			// validateAgent's nil-agentStore check used to silently skip
			// validation (return nil) instead of erroring. A nil agentStore
			// must fail fast here, not be treated as "agent is valid".
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "pending",
				InstanceName: "reassign-no-agent-store",
				Spec:         map[string]any{"cpu": 2},
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

			pub := messaging.NewPublisher(&stubJetStream{})
			noAgentStoreService := rmsvc.NewInstanceService(dataStore, pub, nil)

			err := noAgentStoreService.ReassignAgent(ctx, inst.ID, "test-agent", "")

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(errors.As(err, &svcErr)).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeUnavailable))
		})
	})
})
