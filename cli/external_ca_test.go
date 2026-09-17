package cli

import (
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseExternalCA(t *testing.T) {
	invalidSpecs := []string{
		"",
		"asdf",
		"asdf=",
		"protocol",
		"protocol=foo",
		"protocol=cfssl",
		"url",
		"url=https://xyz",
		"url,protocol",
	}

	for _, spec := range invalidSpecs {
		_, err := parseExternalCA(spec)
		require.Error(t, err)
	}

	validSpecs := []struct {
		input    string
		expected *api.ExternalCA
	}{
		{
			input: "protocol=cfssl,url=https://example.com",
			expected: &api.ExternalCA{
				Protocol: api.ExternalCA_CAProtocolCFSSL,
				URL:      "https://example.com",
				Options:  map[string]string{},
			},
		},
	}

	for _, spec := range validSpecs {
		parsed, err := parseExternalCA(spec.input)
		require.NoError(t, err)
		assert.Equal(t, spec.expected, parsed)
	}
}
