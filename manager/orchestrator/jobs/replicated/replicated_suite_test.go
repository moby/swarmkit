package replicated

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestReplicatedjob(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Replicatedjob Suite")
}
