package secrets

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/identity"
	"github.com/stretchr/testify/assert"
)

func TestTaskRestrictedSecretsProvider(t *testing.T) {
	type testCase struct {
		desc          string
		secretIDs     map[string]struct{}
		secrets       exec.SecretGetter
		secretID      string
		taskID        string
		secretIDToGet string
		value         string
		expected      string
		expectedErr   string
	}

	originalSecretID := identity.NewID()
	taskID := identity.NewID()
	xorID := identity.XorIDs(originalSecretID, taskID)

	testCases := []testCase{
		// The default case when not using a secrets driver or not returning
		// DoNotReuse: true in the SecretsProviderResponse.
		{
			desc:     "Test getting secret by original ID when restricted by task",
			value:    "value",
			expected: "value",
			secretIDs: map[string]struct{}{
				originalSecretID: {},
			},
			// Simulates inserting a secret returned by a driver which sets the
			// DoNotReuse flag to false.
			secretID: originalSecretID,
			// Internal API calls would request to get the secret by the
			// original ID.
			secretIDToGet: originalSecretID,
			taskID:        taskID,
		},
		// The case for when a secrets driver returns DoNotReuse: true in the
		// SecretsProviderResponse.
		{
			desc:     "Test getting secret by xor'ed ID when restricted by task",
			value:    "value",
			expected: "value",
			secretIDs: map[string]struct{}{
				originalSecretID: {},
			},
			// Simulates inserting a secret returned by a driver which sets the
			// DoNotReuse flag to true. This would result in the assignment
			// containing a secret with the ID set to the xor of the secret and
			// task IDs.
			secretID: xorID,
			// Internal API calls would still request to get the secret by the
			// original ID.
			secretIDToGet: originalSecretID,
			taskID:        taskID,
		},
		// This case should catch regressions in the logic coupling of the ID
		// given to secrets in assignments and the corresponding retrieval of
		// the same secrets. If a secret can be got by the xor'ed ID without
		// it being added as such in an assignment, something has been changed
		// inconsistently.
		{
			desc:        "Test attempting to get a secret by xor'ed ID when secret is added with original ID",
			value:       "value",
			expectedErr: fmt.Sprintf("task not authorized to access secret %s", xorID),
			secretIDs: map[string]struct{}{
				originalSecretID: {},
			},
			secretID:      originalSecretID,
			secretIDToGet: xorID,
			taskID:        taskID,
		},
	}
	secretsManager := NewManager()
	for _, testCase := range testCases {
		t.Logf("secretID=%s, taskID=%s, xorID=%s", originalSecretID, taskID, xorID)
		secretsManager.Add(api.Secret{
			ID: testCase.secretID,
			Spec: api.SecretSpec{
				Data: []byte(testCase.value),
			},
		})
		secretsGetter := Restrict(secretsManager, &api.Task{
			ID: taskID,
		})
		(secretsGetter.(*taskRestrictedSecretsProvider)).secretIDs = testCase.secretIDs
		secret, err := secretsGetter.Get(testCase.secretIDToGet)
		if testCase.expectedErr != "" {
			assert.Error(t, err, testCase.desc)
			assert.Equal(t, testCase.expectedErr, err.Error(), testCase.desc)
		} else {
			t.Logf("secretIDs=%v", testCase.secretIDs)
			assert.NoError(t, err, testCase.desc)
			require.NotNil(t, secret, testCase.desc)
			require.NotNil(t, secret.Spec, testCase.desc)
			require.NotNil(t, secret.Spec.Data, testCase.desc)
			assert.Equal(t, testCase.expected, string(secret.Spec.Data), testCase.desc)
		}
		secretsManager.Reset()
	}
}
