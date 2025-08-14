package constraint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	// empty string
	_, err := Parse([]string{""})
	require.Error(t, err)

	_, err = Parse([]string{" "})
	require.Error(t, err)

	// no operator
	_, err = Parse([]string{"nodeabc"})
	require.Error(t, err)

	// incorrect operator
	_, err = Parse([]string{"node ~ abc"})
	require.Error(t, err)

	// Cannot use the leading digit for key
	_, err = Parse([]string{"1node==a2"})
	require.Error(t, err)

	// leading and trailing white space are ignored
	_, err = Parse([]string{" node == node1"})
	require.NoError(t, err)

	// key cannot container white space in the middle
	_, err = Parse([]string{"no de== node1"})
	require.Error(t, err)

	// Cannot use * in key
	_, err = Parse([]string{"no*de==node1"})
	require.Error(t, err)

	// key cannot be empty
	_, err = Parse([]string{"==node1"})
	require.Error(t, err)

	// value cannot be empty
	_, err = Parse([]string{"node=="})
	require.Error(t, err)

	// value cannot be an empty space
	_, err = Parse([]string{"node== "})
	require.Error(t, err)

	// Cannot use $ in key
	_, err = Parse([]string{"no$de==node1"})
	require.Error(t, err)

	// Allow CAPS in key
	exprs, err := Parse([]string{"NoDe==node1"})
	require.NoError(t, err)
	assert.Equal(t, "NoDe", exprs[0].key)

	// Allow dot in key
	exprs, err = Parse([]string{"no.de==node1"})
	require.NoError(t, err)
	assert.Equal(t, "no.de", exprs[0].key)

	// Allow leading underscore
	exprs, err = Parse([]string{"_node==_node1"})
	require.NoError(t, err)
	assert.Equal(t, "_node", exprs[0].key)

	// Allow special characters in exp
	exprs, err = Parse([]string{"node==[a-b]+c*(n|b)/"})
	require.NoError(t, err)
	assert.Equal(t, "node", exprs[0].key)
	assert.Equal(t, exprs[0].exp, "[a-b]+c*(n|b)/")

	// Allow space in Exp
	exprs, err = Parse([]string{"node==node 1"})
	require.NoError(t, err)
	assert.Equal(t, "node", exprs[0].key)
	assert.Equal(t, exprs[0].exp, "node 1")
}

func TestMatch(t *testing.T) {
	exprs, err := Parse([]string{"node.name==foo"})
	require.NoError(t, err)
	e := exprs[0]
	assert.True(t, e.Match("foo"))
	assert.False(t, e.Match("fo"))
	assert.False(t, e.Match("fooE"))

	exprs, err = Parse([]string{"node.name!=foo"})
	require.NoError(t, err)
	e = exprs[0]
	assert.False(t, e.Match("foo"))
	assert.True(t, e.Match("bar"))
	assert.True(t, e.Match("fo"))
	assert.True(t, e.Match("fooExtra"))

	exprs, err = Parse([]string{"node.name==f*o"})
	require.NoError(t, err)
	e = exprs[0]
	assert.False(t, e.Match("fo"))
	assert.True(t, e.Match("f*o"))
	assert.True(t, e.Match("F*o"))
	assert.False(t, e.Match("foo", "fo", "bar"))
	assert.True(t, e.Match("foo", "f*o", "bar"))
	assert.False(t, e.Match("foo"))

	// test special characters
	exprs, err = Parse([]string{"node.name==f.-$o"})
	require.NoError(t, err)
	e = exprs[0]
	assert.False(t, e.Match("fa-$o"))
	assert.True(t, e.Match("f.-$o"))
}
