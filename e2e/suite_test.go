package e2e

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestE2e(t *testing.T) {
	SetDefaultEventuallyTimeout(180 * time.Second)
	RegisterFailHandler(Fail)
	RunSpecs(t, "FDB operator e2e")
}
