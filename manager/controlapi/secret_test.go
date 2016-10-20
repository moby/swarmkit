package controlapi

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func createSecretSpec(name string, data []byte, labels map[string]string) *api.SecretSpec {
	return &api.SecretSpec{
		Annotations: api.Annotations{Name: name, Labels: labels},
		Data:        data,
	}
}

func validateChecksum(t *testing.T, secret *api.Secret, data []byte) {
	checksumBytes := sha256.Sum256(data)
	assert.Equal(t, "sha256:"+hex.EncodeToString(checksumBytes[:]), secret.Digest)
	assert.EqualValues(t, len(data), secret.SecretSize)
}

func TestValidateSecretSpec(t *testing.T) {
	type BadServiceSpec struct {
		spec *api.ServiceSpec
		c    codes.Code
	}

	for _, badName := range []string{
		"",
		".",
		"-",
		"_",
		".name",
		"name.",
		"-name",
		"name-",
		"_name",
		"name_",
		"/a",
		"a/",
		"a/b",
		"..",
		"../a",
		"a/..",
		"withexclamation!",
		"with space",
		"with\nnewline",
		"with@splat",
		"with:colon",
		"with;semicolon",
		"snowman☃",
		strings.Repeat("a", 65),
	} {
		err := validateSecretSpec(createSecretSpec(badName, []byte("valid secret"), nil))
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, grpc.Code(err), grpc.ErrorDesc(err))
	}

	for _, badSpec := range []*api.SecretSpec{
		nil,
		createSecretSpec("validName", nil, nil),
	} {
		err := validateSecretSpec(badSpec)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, grpc.Code(err), grpc.ErrorDesc(err))
	}

	for _, goodName := range []string{
		"0",
		"a",
		"A",
		"name-with--dashes",
		"name.with..dots",
		"name_with__underscores",
		"name.with-all_special",
		"02624name035with1699numbers015125",
		strings.Repeat("a", 64),
	} {
		err := validateSecretSpec(createSecretSpec(goodName, []byte("valid secret"), nil))
		assert.NoError(t, err)
	}

	for _, good := range []*api.SecretSpec{
		createSecretSpec("validName", []byte("☃\n\t\r\x00 dg09236l;kajdgaj5%#9836[Q@!$]"), nil),
		createSecretSpec("validName", []byte("valid secret"), nil),
		createSecretSpec("createName", make([]byte, 1), nil), // 1 byte
	} {
		err := validateSecretSpec(good)
		assert.NoError(t, err)
	}
}

func TestCreateSecret(t *testing.T) {
	s := newTestServer(t)

	// ---- creating a secret with an invalid spec fails, thus checking that CreateSecret validates the spec ----
	_, err := s.Client.CreateSecret(context.Background(), &api.CreateSecretRequest{Spec: createSecretSpec("", nil, nil)})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err), grpc.ErrorDesc(err))

	// ---- creating a secret with a valid spec succeeds, and returns a secret that reflects the secret in the store
	// exactly, but without the private data ----
	data := []byte("secret")
	creationSpec := createSecretSpec("name", data, nil)
	validSpecRequest := api.CreateSecretRequest{Spec: creationSpec}

	resp, err := s.Client.CreateSecret(context.Background(), &validSpecRequest)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Secret)
	validateChecksum(t, resp.Secret, data)

	// the data should be empty/omitted
	assert.Equal(t, *createSecretSpec("name", nil, nil), resp.Secret.Spec)

	// for sanity, check that the stored secret still has the secret data
	var storedSecret *api.Secret
	s.Store.View(func(tx store.ReadTx) {
		storedSecret = store.GetSecret(tx, resp.Secret.ID)
	})
	assert.NotNil(t, storedSecret)
	assert.Equal(t, data, storedSecret.Spec.Data)

	// ---- creating a secret with the same name, even if it's the exact same spec, fails due to a name conflict ----
	_, err = s.Client.CreateSecret(context.Background(), &validSpecRequest)
	assert.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, grpc.Code(err), grpc.ErrorDesc(err))
}

func TestGetSecret(t *testing.T) {
	s := newTestServer(t)

	// ---- getting a secret without providing an ID results in an InvalidArgument ----
	_, err := s.Client.GetSecret(context.Background(), &api.GetSecretRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err), grpc.ErrorDesc(err))

	// ---- getting a non-existent secret fails with NotFound ----
	_, err = s.Client.GetSecret(context.Background(), &api.GetSecretRequest{SecretID: "12345"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err), grpc.ErrorDesc(err))

	// ---- getting an existing secret returns the secret with all the private data cleaned ----
	secret := secretFromSecretSpec(createSecretSpec("name", []byte("data"), nil))
	err = s.Store.Update(func(tx store.Tx) error {
		return store.CreateSecret(tx, secret)
	})
	assert.NoError(t, err)

	resp, err := s.Client.GetSecret(context.Background(), &api.GetSecretRequest{SecretID: secret.ID})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Secret)
	assert.Equal(t, secret, resp.Secret)
}

func TestRemoveSecret(t *testing.T) {
	s := newTestServer(t)

	// ---- removing a secret without providing an ID results in an InvalidArgument ----
	_, err := s.Client.RemoveSecret(context.Background(), &api.RemoveSecretRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err), grpc.ErrorDesc(err))

	// ---- removing a secret that exists succeeds ----
	secret := secretFromSecretSpec(createSecretSpec("name", []byte("data"), nil))
	err = s.Store.Update(func(tx store.Tx) error {
		return store.CreateSecret(tx, secret)
	})
	assert.NoError(t, err)

	resp, err := s.Client.RemoveSecret(context.Background(), &api.RemoveSecretRequest{SecretID: secret.ID})
	assert.NoError(t, err)
	assert.Equal(t, api.RemoveSecretResponse{}, *resp)

	// ---- it was really removed because attempting to remove it again fails with a NotFound ----
	_, err = s.Client.RemoveSecret(context.Background(), &api.RemoveSecretRequest{SecretID: secret.ID})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err), grpc.ErrorDesc(err))

}

func TestListSecrets(t *testing.T) {
	s := newTestServer(t)

	listSecrets := func(req *api.ListSecretsRequest) map[string]*api.Secret {
		resp, err := s.Client.ListSecrets(context.Background(), req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)

		byName := make(map[string]*api.Secret)
		for _, secret := range resp.Secrets {
			byName[secret.Spec.Annotations.Name] = secret
		}
		return byName
	}

	// ---- Listing secrets when there are no secrets returns an empty list but no error ----
	result := listSecrets(&api.ListSecretsRequest{})
	assert.Len(t, result, 0)

	// ---- Create a bunch of secrets in the store so we can test filtering ----
	allListableNames := []string{"aaa", "aab", "abc", "bbb", "bac", "bbc", "ccc", "cac", "cbc", "ddd"}
	secretNamesToID := make(map[string]string)
	for i, secretName := range allListableNames {
		secret := secretFromSecretSpec(createSecretSpec(secretName, []byte("secret"), map[string]string{
			"mod2": fmt.Sprintf("%d", i%2),
			"mod4": fmt.Sprintf("%d", i%4),
		}))
		err := s.Store.Update(func(tx store.Tx) error {
			return store.CreateSecret(tx, secret)
		})
		assert.NoError(t, err)
		secretNamesToID[secretName] = secret.ID
	}
	// also add an internal secret to show that it's never returned
	internalSecret := secretFromSecretSpec(createSecretSpec("internal", []byte("secret"), map[string]string{
		"mod2": "1",
		"mod4": "1",
	}))
	internalSecret.Internal = true
	err := s.Store.Update(func(tx store.Tx) error {
		return store.CreateSecret(tx, internalSecret)
	})
	assert.NoError(t, err)
	secretNamesToID["internal"] = internalSecret.ID

	// ---- build up our list of expectations for what secrets get filtered ----

	type listTestCase struct {
		desc     string
		expected []string
		filter   *api.ListSecretsRequest_Filters
	}

	listSecretTestCases := []listTestCase{
		{
			desc:     "no filter: all the available secrets are returned",
			expected: allListableNames,
			filter:   nil,
		},
		{
			desc:     "searching for something that doesn't match returns an empty list",
			expected: nil,
			filter:   &api.ListSecretsRequest_Filters{Names: []string{"aa", "internal"}},
		},
		{
			desc:     "multiple name filters are or-ed together",
			expected: []string{"aaa", "bbb", "ccc"},
			filter:   &api.ListSecretsRequest_Filters{Names: []string{"aaa", "bbb", "ccc", "internal"}},
		},
		{
			desc:     "multiple name prefix filters are or-ed together",
			expected: []string{"aaa", "aab", "bbb", "bbc"},
			filter:   &api.ListSecretsRequest_Filters{NamePrefixes: []string{"aa", "bb", "int"}},
		},
		{
			desc:     "multiple ID prefix filters are or-ed together",
			expected: []string{"aaa", "bbb"},
			filter: &api.ListSecretsRequest_Filters{IDPrefixes: []string{
				secretNamesToID["aaa"], secretNamesToID["bbb"], secretNamesToID["internal"]},
			},
		},
		{
			desc:     "name prefix, name, and ID prefix filters are or-ed together",
			expected: []string{"aaa", "aab", "bbb", "bbc", "ccc", "ddd"},
			filter: &api.ListSecretsRequest_Filters{
				Names:        []string{"aaa", "ccc", "internal"},
				NamePrefixes: []string{"aa", "bb", "int"},
				IDPrefixes:   []string{secretNamesToID["aaa"], secretNamesToID["ddd"], secretNamesToID["internal"]},
			},
		},
		{
			desc:     "all labels in the label map must be matched",
			expected: []string{allListableNames[0], allListableNames[4], allListableNames[8]},
			filter: &api.ListSecretsRequest_Filters{
				Labels: map[string]string{
					"mod2": "0",
					"mod4": "0",
				},
			},
		},
		{
			desc: "name prefix, name, and ID prefix filters are or-ed together, but the results must match all labels in the label map",
			// + indicates that these would be selected with the name/id/prefix filtering, and 0/1 at the end indicate the mod2 value:
			// +"aaa"0, +"aab"1, "abc"0, +"bbb"1, "bac"0, +"bbc"1, +"ccc"0, "cac"1, "cbc"0, +"ddd"1
			expected: []string{"aaa", "ccc"},
			filter: &api.ListSecretsRequest_Filters{
				Names:        []string{"aaa", "ccc", "internal"},
				NamePrefixes: []string{"aa", "bb", "int"},
				IDPrefixes:   []string{secretNamesToID["aaa"], secretNamesToID["ddd"], secretNamesToID["internal"]},
				Labels: map[string]string{
					"mod2": "0",
				},
			},
		},
	}

	// ---- run the filter tests ----

	for _, expectation := range listSecretTestCases {
		result := listSecrets(&api.ListSecretsRequest{Filters: expectation.filter})
		assert.Len(t, result, len(expectation.expected), expectation.desc)
		for _, name := range expectation.expected {
			assert.Contains(t, result, name, expectation.desc)
			assert.NotNil(t, result[name], expectation.desc)
			assert.Equal(t, secretNamesToID[name], result[name].ID, expectation.desc)
		}
	}
}
