package store_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/dcm-project/control-plane/internal/sp/store/model"
	rmstore "github.com/dcm-project/control-plane/internal/sp/store/resource_manager"
	"github.com/dcm-project/control-plane/internal/sp/testutil"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func postgresAdminDSN() string {
	if dsn := os.Getenv("SP_RM_STORE_POSTGRES_ADMIN_DSN"); dsn != "" {
		return dsn
	}
	return "host=localhost user=admin password=adminpass dbname=postgres port=5432 sslmode=disable"
}

var _ = Describe("ClaimPendingDeletions on Postgres", func() {
	var (
		db     *gorm.DB
		admin  *gorm.DB
		s      rmstore.ServiceTypeInstance
		ctx    context.Context
		testDB string
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		admin, err = gorm.Open(postgres.Open(postgresAdminDSN()), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			Skip("postgres admin connection unavailable: " + err.Error())
		}
		sqlAdmin, err := admin.DB()
		if err != nil || sqlAdmin.Ping() != nil {
			Skip("postgres admin ping failed")
		}

		testDB = "sp_rm_claim_" + uuid.New().String()[:8]
		if err := admin.Exec("CREATE DATABASE " + testDB).Error; err != nil {
			Skip("cannot create postgres test database: " + err.Error())
		}

		db, err = gorm.Open(postgres.Open(fmt.Sprintf(
			"host=localhost user=admin password=adminpass dbname=%s port=5432 sslmode=disable",
			testDB,
		)), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&model.ServiceTypeInstance{})).To(Succeed())
		s = rmstore.NewServiceTypeInstance(db, testutil.FastServiceTypeInstanceRetry()...)
	})

	AfterEach(func() {
		if db != nil {
			sqlDB, err := db.DB()
			if err == nil {
				_ = sqlDB.Close()
			}
		}
		if admin != nil && testDB != "" {
			_ = admin.Exec("DROP DATABASE IF EXISTS " + testDB).Error
			sqlAdmin, err := admin.DB()
			if err == nil {
				_ = sqlAdmin.Close()
			}
		}
	})

	It("uses SKIP LOCKED so concurrent claimers do not take the same row", func() {
		inst1, err := s.Create(ctx, newServiceTypeInstance("pg-claim-1", map[string]any{}))
		Expect(err).NotTo(HaveOccurred())
		inst2, err := s.Create(ctx, newServiceTypeInstance("pg-claim-2", map[string]any{}))
		Expect(err).NotTo(HaveOccurred())
		Expect(s.MarkForDeletion(ctx, inst1.ID)).To(Succeed())
		Expect(s.MarkForDeletion(ctx, inst2.ID)).To(Succeed())

		now := time.Now()
		claimUntil := now.Add(5 * time.Minute)

		var (
			wg       sync.WaitGroup
			claimedA []model.ServiceTypeInstance
			claimedB []model.ServiceTypeInstance
			errA     error
			errB     error
		)
		wg.Add(2)
		go func() {
			claimedA, errA = s.ClaimPendingDeletions(ctx, now, claimUntil, 0)
			wg.Done()
		}()
		go func() {
			claimedB, errB = s.ClaimPendingDeletions(ctx, now, claimUntil, 0)
			wg.Done()
		}()
		wg.Wait()

		Expect(errA).NotTo(HaveOccurred())
		Expect(errB).NotTo(HaveOccurred())

		seen := make(map[string]int)
		for _, inst := range append(claimedA, claimedB...) {
			seen[inst.ID]++
		}
		Expect(seen).To(HaveLen(2))
		Expect(seen[inst1.ID]).To(Equal(1))
		Expect(seen[inst2.ID]).To(Equal(1))
	})
})
