package globaljob_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestGlobaljob(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Globaljob Suite")
}
