package globaljob_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/docker/swarmkit/manager/orchestrator/globaljob"

	"context"

	"github.com/docker/swarmkit/manager/orchestrator/testutils"
)

var _ = Describe("Global Job Orchestrator", func() {
	var (
		o *Orchestrator
	)

	It("should stop when Stop is called", func(done Done) {
		o = NewOrchestrator(nil)
		stopped := testutils.EnsureRuns(func() { o.Run(context.Background()) })
		o.Stop()
		Expect(stopped).To(BeClosed())
		close(done)
	})
})
