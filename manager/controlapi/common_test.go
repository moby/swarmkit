package controlapi

import (
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func TestValidateAnnotations(t *testing.T) {
	err := validateAnnotations(api.Annotations{})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	for _, good := range []api.Annotations{
		{Name: "name"},
		{Name: "n-me"},
		{Name: "n_me"},
		{Name: "n-m-e"},
		{Name: "n--d"},
	} {
		err := validateAnnotations(good)
		require.NoError(t, err, "string: "+good.Name)
	}

	for _, bad := range []api.Annotations{
		{Name: "_nam"},
		{Name: ".nam"},
		{Name: "-nam"},
		{Name: "nam-"},
		{Name: "n/me"},
		{Name: "n&me"},
		{Name: "////"},
	} {
		err := validateAnnotations(bad)
		require.Error(t, err, "string: "+bad.Name)
	}
}
