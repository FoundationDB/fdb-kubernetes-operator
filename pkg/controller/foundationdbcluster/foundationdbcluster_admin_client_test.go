package foundationdbcluster

import (
	"testing"

	"github.com/onsi/gomega"
)

func TestSerializingLocalityPolicies(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	var policy localityPolicy
	policy = &singletonPolicy{}
	g.Expect(policy.BinaryRepresentation()).To(gomega.Equal([]byte("\x03\x00\x00\x00One")))
	policy = &acrossPolicy{
		Count:     2,
		Field:     "zoneid",
		Subpolicy: &singletonPolicy{},
	}
	g.Expect(policy.BinaryRepresentation()).To(gomega.Equal([]byte("\x06\x00\x00\x00Across\x06\x00\x00\x00zoneid\x02\x00\x00\x00\x03\x00\x00\x00One")))
}
