package port_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestPort(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Port Suite")
}
