package node

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	cautils "github.com/docker/swarmkit/ca/testutils"
	"github.com/docker/swarmkit/identity"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

// If there is nothing on disk and no join addr, we create a new CA and a new set of TLS certs.
// If AutoLockManagers is enabled, the TLS key is encrypted with a randomly generated lock key.
func TestLoadSecurityConfigNewNode(t *testing.T) {
	for _, autoLockManagers := range []bool{true, false} {
		tempdir, err := ioutil.TempDir("", "test-new-node")
		require.NoError(t, err)
		defer os.RemoveAll(tempdir)

		paths := ca.NewConfigPaths(filepath.Join(tempdir, "certificates"))

		node, err := New(&Config{
			StateDir:         tempdir,
			AutoLockManagers: autoLockManagers,
		})
		require.NoError(t, err)
		securityConfig, err := node.loadSecurityConfig(context.Background())
		require.NoError(t, err)
		require.NotNil(t, securityConfig)

		unencryptedReader := ca.NewKeyReadWriter(paths.Node, nil, nil)
		_, _, err = unencryptedReader.Read()
		if !autoLockManagers {
			require.NoError(t, err)
		} else {
			require.IsType(t, ca.ErrInvalidKEK{}, err)
		}
	}
}

// If there's only a root CA on disk (no TLS certs), and no join addr, we create a new CA
// and a new set of TLS certs.  Similarly if there's only a TLS cert and key, and no CA.
func TestLoadSecurityConfigPartialCertsOnDisk(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "test-new-node")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	paths := ca.NewConfigPaths(filepath.Join(tempdir, "certificates"))
	rootCA, err := ca.CreateRootCA(ca.DefaultRootCN, paths.RootCA)
	require.NoError(t, err)

	node, err := New(&Config{
		StateDir: tempdir,
	})
	require.NoError(t, err)
	securityConfig, err := node.loadSecurityConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, securityConfig)

	cert, key, err := securityConfig.KeyReader().Read()
	require.NoError(t, err)

	// a new CA was generated because no existing TLS certs were present
	require.NotEqual(t, rootCA.Cert, securityConfig.RootCA().Cert)

	// if the TLS key and cert are on disk, but there's no CA, a new CA and TLS
	// key+cert are generated
	require.NoError(t, os.RemoveAll(paths.RootCA.Cert))

	node, err = New(&Config{
		StateDir: tempdir,
	})
	require.NoError(t, err)
	securityConfig, err = node.loadSecurityConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, securityConfig)

	newCert, newKey, err := securityConfig.KeyReader().Read()
	require.NoError(t, err)
	require.NotEqual(t, cert, newCert)
	require.NotEqual(t, key, newKey)
}

// If there are CAs and TLS certs on disk, it tries to load and fails if there
// are any errors, even if a join token is provided.
func TestLoadSecurityConfigLoadFromDisk(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "test-load-node-tls")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	paths := ca.NewConfigPaths(filepath.Join(tempdir, "certificates"))

	tc := cautils.NewTestCA(t)
	defer tc.Stop()
	peer, err := tc.ConnBroker.Remotes().Select()
	require.NoError(t, err)

	// Load successfully with valid passphrase
	rootCA, err := ca.CreateRootCA(ca.DefaultRootCN, paths.RootCA)
	require.NoError(t, err)
	krw := ca.NewKeyReadWriter(paths.Node, []byte("passphrase"), nil)
	require.NoError(t, err)
	_, err = rootCA.IssueAndSaveNewCertificates(krw, identity.NewID(), ca.WorkerRole, identity.NewID())
	require.NoError(t, err)

	node, err := New(&Config{
		StateDir:  tempdir,
		JoinAddr:  peer.Addr,
		JoinToken: tc.ManagerToken,
		UnlockKey: []byte("passphrase"),
	})
	require.NoError(t, err)
	securityConfig, err := node.loadSecurityConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, securityConfig)

	// Invalid passphrase
	node, err = New(&Config{
		StateDir:  tempdir,
		JoinAddr:  peer.Addr,
		JoinToken: tc.ManagerToken,
	})
	require.NoError(t, err)
	_, err = node.loadSecurityConfig(context.Background())
	require.Equal(t, ErrInvalidUnlockKey, err)

	// Invalid CA
	rootCA, err = ca.CreateRootCA(ca.DefaultRootCN, paths.RootCA)
	require.NoError(t, err)
	node, err = New(&Config{
		StateDir:  tempdir,
		JoinAddr:  peer.Addr,
		JoinToken: tc.ManagerToken,
		UnlockKey: []byte("passphrase"),
	})
	require.NoError(t, err)
	_, err = node.loadSecurityConfig(context.Background())
	require.IsType(t, x509.UnknownAuthorityError{}, errors.Cause(err))
}

// If there is no CA, and a join addr is provided, one is downloaded from the
// join server. If there is a CA, it is just loaded from disk.  The TLS key and
// cert are also downloaded.
func TestLoadSecurityConfigDownloadAllCerts(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "test-join-node")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	paths := ca.NewConfigPaths(filepath.Join(tempdir, "certificates"))

	// join addr is invalid
	node, err := New(&Config{
		StateDir: tempdir,
		JoinAddr: "127.0.0.1:12",
	})
	require.NoError(t, err)
	_, err = node.loadSecurityConfig(context.Background())
	require.Error(t, err)

	tc := cautils.NewTestCA(t)
	defer tc.Stop()

	peer, err := tc.ConnBroker.Remotes().Select()
	require.NoError(t, err)

	node, err = New(&Config{
		StateDir:  tempdir,
		JoinAddr:  peer.Addr,
		JoinToken: tc.ManagerToken,
	})
	require.NoError(t, err)
	_, err = node.loadSecurityConfig(context.Background())
	require.NoError(t, err)

	// the TLS key and cert were written to disk unencrypted
	_, _, err = ca.NewKeyReadWriter(paths.Node, nil, nil).Read()
	require.NoError(t, err)

	// remove the TLS cert and key, and mark the root CA cert so that we will
	// know if it gets replaced
	require.NoError(t, os.Remove(paths.Node.Cert))
	require.NoError(t, os.Remove(paths.Node.Key))
	certBytes, err := ioutil.ReadFile(paths.RootCA.Cert)
	require.NoError(t, err)
	pemBlock, _ := pem.Decode(certBytes)
	require.NotNil(t, pemBlock)
	pemBlock.Headers["marked"] = "true"
	certBytes = pem.EncodeToMemory(pemBlock)
	require.NoError(t, ioutil.WriteFile(paths.RootCA.Cert, certBytes, 0644))

	// also make sure the new set gets downloaded and written to disk with a passphrase
	// by updating the memory store with manager autolock on and an unlock key
	require.NoError(t, tc.MemoryStore.Update(func(tx store.Tx) error {
		clusters, err := store.FindClusters(tx, store.All)
		require.NoError(t, err)
		require.Len(t, clusters, 1)

		newCluster := clusters[0].Copy()
		newCluster.Spec.EncryptionConfig.AutoLockManagers = true
		newCluster.UnlockKeys = []*api.EncryptionKey{{
			Subsystem: ca.ManagerRole,
			Key:       []byte("passphrase"),
		}}
		return store.UpdateCluster(tx, newCluster)
	}))

	// Join with without any passphrase - this should be fine, because the TLS
	// key is downloaded and then loaded just fine.  However, it *is* written
	// to disk encrypted.
	node, err = New(&Config{
		StateDir:  tempdir,
		JoinAddr:  peer.Addr,
		JoinToken: tc.ManagerToken,
	})
	require.NoError(t, err)
	_, err = node.loadSecurityConfig(context.Background())
	require.NoError(t, err)

	// make sure the CA cert has not been replaced
	readCertBytes, err := ioutil.ReadFile(paths.RootCA.Cert)
	require.NoError(t, err)
	require.Equal(t, certBytes, readCertBytes)

	// the TLS node cert and key were saved to disk encrypted, though
	_, _, err = ca.NewKeyReadWriter(paths.Node, nil, nil).Read()
	require.Error(t, err)
	_, _, err = ca.NewKeyReadWriter(paths.Node, []byte("passphrase"), nil).Read()
	require.NoError(t, err)
}
