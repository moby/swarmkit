package ca

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	cfcsr "github.com/cloudflare/cfssl/csr"
	"github.com/cloudflare/cfssl/helpers"
	"github.com/phayes/permbits"
	"github.com/stretchr/testify/assert"
)

func TestCreateRootCA(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	pathToRootCACert := filepath.Join(tempBaseDir, "root.crt")
	pathToRootCAKey := filepath.Join(tempBaseDir, "root.key")

	_, _, err = CreateRootCA(pathToRootCACert, pathToRootCAKey, "rootCN")
	assert.NoError(t, err)

	perms, err := permbits.Stat(pathToRootCACert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
	perms, err = permbits.Stat(pathToRootCAKey)
	assert.NoError(t, err)
	assert.False(t, perms.GroupRead())
	assert.False(t, perms.OtherRead())
}

func TestGetRootCA(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	pathToRootCACert := filepath.Join(tempBaseDir, "root.crt")
	pathToRootCAKey := filepath.Join(tempBaseDir, "root.key")

	_, rootCACert, err := CreateRootCA(pathToRootCACert, pathToRootCAKey, "rootCN")
	assert.NoError(t, err)

	rootCACertificate, err := GetRootCA(pathToRootCACert)
	assert.NoError(t, err)
	assert.Equal(t, rootCACert, rootCACertificate)
}

func TestGenerateAndSignNewTLSCert(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	pathToRootCACert := filepath.Join(tempBaseDir, "root.crt")
	pathToRootCAKey := filepath.Join(tempBaseDir, "root.key")
	pathToCert := filepath.Join(tempBaseDir, "cert.crt")
	pathToKey := filepath.Join(tempBaseDir, "cert.key")

	signer, rootCACert, err := CreateRootCA(pathToRootCACert, pathToRootCAKey, "rootCN")
	assert.NoError(t, err)

	_, err = GenerateAndSignNewTLSCert(signer, rootCACert, pathToCert, pathToKey, "CN", "OU")
	assert.NoError(t, err)

	perms, err := permbits.Stat(pathToCert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
	perms, err = permbits.Stat(pathToKey)
	assert.NoError(t, err)
	assert.False(t, perms.GroupRead())
	assert.False(t, perms.OtherRead())
}

func TestGenerateAndWriteNewCSR(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	pathToCSR := filepath.Join(tempBaseDir, "cert.csr")
	pathToKey := filepath.Join(tempBaseDir, "cert.key")

	csr, key, err := GenerateAndWriteNewCSR(pathToCSR, pathToKey)
	assert.NoError(t, err)
	assert.NotNil(t, csr)
	assert.NotNil(t, key)

	perms, err := permbits.Stat(pathToCSR)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
	perms, err = permbits.Stat(pathToKey)
	assert.NoError(t, err)
	assert.False(t, perms.GroupRead())
	assert.False(t, perms.OtherRead())

	_, err = helpers.ParseCSRPEM(csr)
	assert.NoError(t, err)
}

func TestParseValidateAndSignCSR(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	pathToRootCACert := filepath.Join(tempBaseDir, "root.crt")
	pathToRootCAKey := filepath.Join(tempBaseDir, "root.key")

	signer, _, err := CreateRootCA(pathToRootCACert, pathToRootCAKey, "rootCN")
	assert.NoError(t, err)

	csr, _, err := generateNewCSR()
	assert.NoError(t, err)

	signedCert, err := ParseValidateAndSignCSR(signer, csr, "CN", "OU")
	assert.NoError(t, err)
	assert.NotNil(t, signedCert)

	parsedCert, err := helpers.ParseCertificatesPEM(signedCert)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(parsedCert))
	assert.Equal(t, "CN", parsedCert[0].Subject.CommonName)
	assert.Equal(t, 1, len(parsedCert[0].Subject.OrganizationalUnit))
	assert.Equal(t, "OU", parsedCert[0].Subject.OrganizationalUnit[0])
	assert.Equal(t, 2, len(parsedCert[0].Subject.Names))
}

func TestParseValidateAndSignMaliciousCSR(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	pathToRootCACert := filepath.Join(tempBaseDir, "root.crt")
	pathToRootCAKey := filepath.Join(tempBaseDir, "root.key")

	signer, _, err := CreateRootCA(pathToRootCACert, pathToRootCAKey, "rootCN")
	assert.NoError(t, err)

	req := &cfcsr.CertificateRequest{
		Names: []cfcsr.Name{
			{
				O:  "maliciousOrg",
				OU: "maliciousOu",
			},
		},
		CN:         "maliciousCN",
		Hosts:      []string{"docker.com"},
		KeyRequest: &cfcsr.BasicKeyRequest{A: "ecdsa", S: 256},
	}

	csr, _, err := cfcsr.ParseRequest(req)
	assert.NoError(t, err)

	signedCert, err := ParseValidateAndSignCSR(signer, csr, "CN", "OU")
	assert.NoError(t, err)
	assert.NotNil(t, signedCert)

	parsedCert, err := helpers.ParseCertificatesPEM(signedCert)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(parsedCert))
	assert.Equal(t, "CN", parsedCert[0].Subject.CommonName)
	assert.Equal(t, 1, len(parsedCert[0].Subject.OrganizationalUnit))
	assert.Equal(t, "OU", parsedCert[0].Subject.OrganizationalUnit[0])
	assert.Equal(t, 2, len(parsedCert[0].Subject.Names))
	assert.Empty(t, "", parsedCert[0].Subject.Organization)
}

func TestGetRemoteCA(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	pathToRootCACert := filepath.Join(tempBaseDir, "root.crt")
	pathToRootCAKey := filepath.Join(tempBaseDir, "root.key")
	pathToCert := filepath.Join(tempBaseDir, "cert.crt")
	pathToKey := filepath.Join(tempBaseDir, "cert.key")

	signer, rootCACert, err := CreateRootCA(pathToRootCACert, pathToRootCAKey, "rootCN")
	assert.NoError(t, err)

	tlsCert, err := GenerateAndSignNewTLSCert(signer, rootCACert, pathToCert, pathToKey, "CN", "OU")
	assert.NoError(t, err)

	config := &tls.Config{
		Certificates: []tls.Certificate{*tlsCert},
		MinVersion:   tls.VersionTLS12,
	}

	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.TLS = config
	ts.StartTLS()
	defer ts.Close()

	shaHash := sha256.New()
	shaHash.Write(rootCACert)
	md := shaHash.Sum(nil)
	mdStr := hex.EncodeToString(md)

	_, err = GetRemoteCA(ts.Listener.Addr().String(), mdStr)
	assert.NoError(t, err)
}

func TestGetRemoteCAInvalidHash(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	pathToRootCACert := filepath.Join(tempBaseDir, "root.crt")
	pathToRootCAKey := filepath.Join(tempBaseDir, "root.key")
	pathToCert := filepath.Join(tempBaseDir, "cert.crt")
	pathToKey := filepath.Join(tempBaseDir, "cert.key")

	signer, rootCACert, err := CreateRootCA(pathToRootCACert, pathToRootCAKey, "rootCN")
	assert.NoError(t, err)

	tlsCert, err := GenerateAndSignNewTLSCert(signer, rootCACert, pathToCert, pathToKey, "CN", "OU")
	assert.NoError(t, err)

	config := &tls.Config{
		Certificates: []tls.Certificate{*tlsCert},
		MinVersion:   tls.VersionTLS12,
	}

	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.TLS = config
	ts.StartTLS()
	defer ts.Close()

	_, err = GetRemoteCA(ts.Listener.Addr().String(), "2d2f968475269f0dde5299427cf74348ee1d6115b95c6e3f283e5a4de8da445b")
	assert.Error(t, err)
}
