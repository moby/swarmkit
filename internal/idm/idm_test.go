package idm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	i, err := New(0, 10)
	require.NoErrorf(t, err, "idm.New(0, 10) error = %v", err)
	assert.NotNil(t, i.set, "set is not initialized")
	assert.Equalf(t, 0, i.start, "unexpected start: got %d, want 0", i.start)
	assert.Equalf(t, 10, i.end, "unexpected end: got %d, want 10", i.end)
}

func TestAllocate(t *testing.T) {
	i, err := New(50, 52)
	require.NoError(t, err)

	err = i.GetSpecificID(49)
	require.Error(t, err, "i.GetSpecificID(49): expected failure but succeeded")

	err = i.GetSpecificID(53)
	require.Error(t, err, "i.GetSpecificID(53): expected failure but succeeded")

	o, err := i.GetID(false)
	require.NoErrorf(t, err, "i.GetID(false) error = %v", err)
	assert.Equalf(t, 50, o, "i.GetID(false) = %v, want 50", o)

	err = i.GetSpecificID(50)
	require.Error(t, err, "i.GetSpecificID(50): allocating already-allocated id should fail")

	o, err = i.GetID(false)
	require.NoErrorf(t, err, "i.GetID(false) error = %v", err)
	assert.Equalf(t, 51, o, "i.GetID(false) = %v, want 51", o)

	o, err = i.GetID(false)
	require.NoErrorf(t, err, "i.GetID(false) error = %v", err)
	assert.Equalf(t, 52, o, "i.GetID(false) = %v, want 52", o)

	o, err = i.GetID(false)
	require.Errorf(t, err, "i.GetID(false) = %v, allocating ID from full set should fail", o)

	i.Release(50)

	o, err = i.GetID(false)
	require.NoErrorf(t, err, "i.GetID(false) error = %v", err)
	assert.Equalf(t, 50, o, "i.GetID(false) = %v, want 50", o)

	i.Release(52)
	err = i.GetSpecificID(52)
	assert.NoErrorf(t, err, "i.GetSpecificID(52) error = %v, expected success allocating a released ID", err)
}

func TestUninitialized(t *testing.T) {
	i := &IDM{}

	_, err := i.GetID(false)
	require.Error(t, err, "i.GetID(...) on uninitialized set should fail")

	err = i.GetSpecificID(44)
	assert.Error(t, err, "i.GetSpecificID(...) on uninitialized set should fail")
}

func TestAllocateSerial(t *testing.T) {
	i, err := New(50, 55)
	require.NoErrorf(t, err, "New(50, 55) error = %v", err)

	err = i.GetSpecificID(49)
	require.Errorf(t, err, "i.GetSpecificID(49): allocating out-of-range id should fail")

	err = i.GetSpecificID(56)
	require.Errorf(t, err, "i.GetSpecificID(56): allocating out-of-range id should fail")

	o, err := i.GetID(true)
	require.NoErrorf(t, err, "i.GetID(true) error = %v", err)
	assert.Equalf(t, 50, o, "i.GetID(true) = %v, want 50", o)

	err = i.GetSpecificID(50)
	require.Errorf(t, err, "i.GetSpecificID(50): allocating already-allocated id should fail")

	o, err = i.GetID(true)
	require.NoErrorf(t, err, "i.GetID(true) error = %v", err)
	assert.Equalf(t, 51, o, "i.GetID(true) = %v, want 51", o)

	o, err = i.GetID(true)
	require.NoErrorf(t, err, "i.GetID(true) error = %v", err)
	assert.Equalf(t, 52, o, "i.GetID(true) = %v, want 52", o)

	i.Release(50)

	o, err = i.GetID(true)
	require.NoErrorf(t, err, "i.GetID(true) error = %v", err)
	assert.Equalf(t, 53, o, "i.GetID(true) = %v, want 53", o)

	i.Release(52)
	err = i.GetSpecificID(52)
	assert.NoErrorf(t, err, "i.GetSpecificID(52) error = %v, expected success allocating a released ID", err)
}
