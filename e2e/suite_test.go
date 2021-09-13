// +build e2e

package e2e

import (
	"log"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestE2e(t *testing.T) {
	SetDefaultEventuallyTimeout(180 * time.Second)
	RegisterFailHandler(Fail)
	RunSpecs(t, "[e2e] FDB operator e2e")
	log.SetOutput(GinkgoWriter)
}
