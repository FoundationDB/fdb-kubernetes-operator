package internal

import (
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("[internal] deprecations", func() {
	When("Providing a custom parameter", func() {
		type testCase struct {
			Input              []string
			ExpectedViolations []string
		}

		DescribeTable("should set the expected parameter or print the violation",
			func(tc testCase) {
				err := ValidateCustomParameters(tc.Input)
				var errMsg error
				if len(tc.ExpectedViolations) > 0 {
					errMsg = fmt.Errorf("found the following customParameters violations:\n%s", strings.Join(tc.ExpectedViolations, "\n"))
				}

				if err == nil {
					Expect(len(tc.ExpectedViolations)).To(BeNumerically("==", 0))
				} else {
					Expect(err).To(Equal(errMsg))
				}
			},
			Entry("Valid parameter without duplicate",
				testCase{
					Input:              []string{"blahblah=1"},
					ExpectedViolations: []string{},
				}),
			Entry("Valid parameter with duplicate",
				testCase{
					Input:              []string{"blahblah=1", "blahblah=1"},
					ExpectedViolations: []string{"found duplicated customParameter: blahblah"},
				}),
			Entry("Protected parameter without duplicate",
				testCase{
					Input:              []string{"datadir=1"},
					ExpectedViolations: []string{"found protected customParameter: datadir, please remove this parameter from the customParameters list"},
				}),
			Entry("Valid parameter with duplicate and protected parameter",
				testCase{
					Input:              []string{"blahblah=1", "blahblah=1", "datadir=1"},
					ExpectedViolations: []string{"found duplicated customParameter: blahblah", "found protected customParameter: datadir, please remove this parameter from the customParameters list"},
				}),
		)
	})
})
