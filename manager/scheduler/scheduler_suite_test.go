package scheduler

// scheduler_suite_test.go contains the boilerplate for the ginkgo tests of the
// scheduler and associated components

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// TestSchedulerGinkgo is the boilerplate that starts the Ginkgo tests of the
// scheduler.
func TestSchedulerGinkgo(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Scheduler Ginkgo Suite")
}
