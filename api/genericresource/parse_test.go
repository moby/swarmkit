package genericresource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDiscrete(t *testing.T) {
	res, err := ParseCmd("apple=3")
	require.NoError(t, err)
	assert.Len(t, res, 1)

	apples := GetResource("apple", res)
	assert.Len(t, apples, 1)
	assert.Equal(t, int64(3), apples[0].GetDiscreteResourceSpec().Value)

	_, err = ParseCmd("apple=3\napple=4")
	require.Error(t, err)

	_, err = ParseCmd("apple=3,apple=4")
	require.Error(t, err)

	_, err = ParseCmd("apple=-3")
	require.Error(t, err)
}

func TestParseStr(t *testing.T) {
	res, err := ParseCmd("orange=red,orange=green,orange=blue")
	require.NoError(t, err)
	assert.Len(t, res, 3)

	oranges := GetResource("orange", res)
	assert.Len(t, oranges, 3)
	for _, k := range []string{"red", "green", "blue"} {
		assert.True(t, HasResource(NewString("orange", k), oranges))
	}
}

func TestParseDiscreteAndStr(t *testing.T) {
	res, err := ParseCmd("orange=red,orange=green,orange=blue,apple=3")
	require.NoError(t, err)
	assert.Len(t, res, 4)

	oranges := GetResource("orange", res)
	assert.Len(t, oranges, 3)
	for _, k := range []string{"red", "green", "blue"} {
		assert.True(t, HasResource(NewString("orange", k), oranges))
	}

	apples := GetResource("apple", res)
	assert.Len(t, apples, 1)
	assert.Equal(t, int64(3), apples[0].GetDiscreteResourceSpec().Value)
}
