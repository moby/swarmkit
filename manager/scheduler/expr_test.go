package scheduler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseExprs(t *testing.T) {
	// empty string
	_, err := ParseExprs([]string{""})
	assert.Error(t, err)

	_, err = ParseExprs([]string{" "})
	assert.Error(t, err)

	// no operator
	_, err = ParseExprs([]string{"nodeabc"})
	assert.Error(t, err)

	// incorrect operator
	_, err = ParseExprs([]string{"node ~ abc"})
	assert.Error(t, err)

	// Cannot use the leading digit for Key
	_, err = ParseExprs([]string{"1node==a2"})
	assert.Error(t, err)

	// leading and trailing white space are ignored
	_, err = ParseExprs([]string{" node == node1"})
	assert.NoError(t, err)

	// Key cannot container white space in the middle
	_, err = ParseExprs([]string{"no de== node1"})
	assert.Error(t, err)

	// Cannot use * in Key
	_, err = ParseExprs([]string{"no*de==node1"})
	assert.Error(t, err)

	// Key cannot be empty
	_, err = ParseExprs([]string{"==node1"})
	assert.Error(t, err)

	// value cannot be empty
	_, err = ParseExprs([]string{"node=="})
	assert.Error(t, err)

	// value cannot be an empty space
	_, err = ParseExprs([]string{"node== "})
	assert.Error(t, err)

	// Cannot use $ in Key
	_, err = ParseExprs([]string{"no$de==node1"})
	assert.Error(t, err)

	// Allow CAPS in Key
	exprs, err := ParseExprs([]string{"NoDe==node1"})
	assert.NoError(t, err)
	assert.Equal(t, exprs[0].Key, "NoDe")

	// Allow dot in Key
	exprs, err = ParseExprs([]string{"no.de==node1"})
	assert.NoError(t, err)
	assert.Equal(t, exprs[0].Key, "no.de")

	// Allow leading underscore
	exprs, err = ParseExprs([]string{"_node==_node1"})
	assert.NoError(t, err)
	assert.Equal(t, exprs[0].Key, "_node")

	// Allow special characters in exp
	exprs, err = ParseExprs([]string{"node==[a-b]+c*(n|b)/"})
	assert.NoError(t, err)
	assert.Equal(t, exprs[0].Key, "node")
	assert.Equal(t, exprs[0].exp, "[a-b]+c*(n|b)/")

	// Allow space in Exp
	exprs, err = ParseExprs([]string{"node==node 1"})
	assert.NoError(t, err)
	assert.Equal(t, exprs[0].Key, "node")
	assert.Equal(t, exprs[0].exp, "node 1")
}

func TestMatch(t *testing.T) {
	exprs, err := ParseExprs([]string{"node.name==foo"})
	assert.NoError(t, err)
	e := exprs[0]
	assert.True(t, e.Match("foo"))
	assert.False(t, e.Match("fo"))
	assert.False(t, e.Match("fooE"))

	exprs, err = ParseExprs([]string{"node.name!=foo"})
	assert.NoError(t, err)
	e = exprs[0]
	assert.False(t, e.Match("foo"))
	assert.True(t, e.Match("bar"))
	assert.True(t, e.Match("fo"))
	assert.True(t, e.Match("fooExtra"))

	exprs, err = ParseExprs([]string{"node.name==f*o"})
	assert.NoError(t, err)
	e = exprs[0]
	assert.False(t, e.Match("fo"))
	assert.True(t, e.Match("f*o"))
	assert.True(t, e.Match("F*o"))
	assert.False(t, e.Match("foo", "fo", "bar"))
	assert.True(t, e.Match("foo", "f*o", "bar"))
	assert.False(t, e.Match("foo"))

	// test special characters
	exprs, err = ParseExprs([]string{"node.name==f.-$o"})
	assert.NoError(t, err)
	e = exprs[0]
	assert.False(t, e.Match("fa-$o"))
	assert.True(t, e.Match("f.-$o"))
}
