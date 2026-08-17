package cel_test

import (
	"github.com/dcm-project/control-plane/internal/cel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ParseReference", func() {
	It("parses a valid CEL reference", func() {
		ref, isReference, err := cel.ParseReference("${db.connection_string}")
		Expect(err).NotTo(HaveOccurred())
		Expect(isReference).To(BeTrue())
		Expect(ref.ResourceName).To(Equal("db"))
		Expect(ref.OutputField).To(Equal("connection_string"))
	})

	It("parses dotted output paths", func() {
		ref, isReference, err := cel.ParseReference("${db.config.host}")
		Expect(err).NotTo(HaveOccurred())
		Expect(isReference).To(BeTrue())
		Expect(ref.ResourceName).To(Equal("db"))
		Expect(ref.OutputField).To(Equal("config.host"))
	})

	It("parses indexed output paths", func() {
		ref, isReference, err := cel.ParseReference("${db.disks[0].name}")
		Expect(err).NotTo(HaveOccurred())
		Expect(isReference).To(BeTrue())
		Expect(ref.ResourceName).To(Equal("db"))
		Expect(ref.OutputField).To(Equal("disks[0].name"))
	})

	It("returns isReference=false for plain strings", func() {
		_, isReference, err := cel.ParseReference("postgres://localhost/db")
		Expect(err).NotTo(HaveOccurred())
		Expect(isReference).To(BeFalse())
	})

	It("rejects embedded CEL syntax", func() {
		_, isReference, err := cel.ParseReference("url-${db.connection_string}")
		Expect(err).To(HaveOccurred())
		Expect(isReference).To(BeTrue())
	})
})
