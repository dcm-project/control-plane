package store_test

import (
	"github.com/dcm-project/control-plane/internal/policy/config"
	"github.com/dcm-project/control-plane/internal/policy/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("InitDB", func() {
	It("initializes SQLite database", func() {
		cfg := &config.Config{
			Database: &config.DBConfig{
				Type: "sqlite",
				Name: ":memory:",
			},
		}

		db, err := store.InitDB(cfg)

		Expect(err).NotTo(HaveOccurred())
		Expect(db).NotTo(BeNil())

		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
})
