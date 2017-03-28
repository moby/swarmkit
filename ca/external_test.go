package ca_test

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/docker/swarmkit/ca"
	"github.com/stretchr/testify/require"
)

func TestExternalCASignRequestTimesOut(t *testing.T) {
	t.Parallel()

	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateRootCA("rootCN", paths.RootCA)
	require.NoError(t, err)

	signDone, allDone := make(chan error), make(chan struct{})
	defer close(signDone)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(http.ResponseWriter, *http.Request) {
		// hang forever
		select {
		case <-allDone:
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	defer server.CloseClientConnections()
	defer close(allDone)

	csr, _, err := ca.GenerateNewCSR()
	require.NoError(t, err)

	externalCA := ca.NewExternalCA(&rootCA, nil, server.URL)
	externalCA.ExternalRequestTimeout = time.Second
	go func() {
		_, err := externalCA.Sign(context.Background(), ca.PrepareCSR(csr, "cn", "ou", "org"))
		select {
		case <-allDone:
		case signDone <- err:
		}
	}()

	select {
	case err = <-signDone:
		require.Contains(t, err.Error(), context.DeadlineExceeded.Error())
	case <-time.After(3 * time.Second):
		require.FailNow(t, "call to external CA signing should have timed out after 1 second - it's been 3")
	}
}
