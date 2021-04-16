package e2e

import (
. "github.com/onsi/ginkgo"
. "github.com/onsi/gomega"
"testing"
	"time"
)

func TestE2e(t *testing.T) {
	SetDefaultEventuallyTimeout(180 * time.Second)
	RegisterFailHandler(Fail)
	RunSpecs(t, "FDB operator e2e")
}
