package encryption

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodeDecode(t *testing.T) {
	// not providing an encoder will fail
	msg := []byte("hello again swarmkit")
	_, err := Encode(msg, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no encoder")

	// noop encoder can encode
	encoded, err := Encode(msg, NoopCoder)
	require.NoError(t, err)

	// not providing a decoder will fail
	_, err = Decode(encoded, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no decoder")

	// noop decoder can decode
	decoded, err := Decode(encoded, NoopCoder)
	require.NoError(t, err)
	require.Equal(t, msg, decoded)

	// the default encoder can produce something the default decoder can read
	encoder, decoder := Defaults([]byte("key"))
	encoded, err = Encode(msg, encoder)
	require.NoError(t, err)
	decoded, err = Decode(encoded, decoder)
	require.NoError(t, err)
	require.Equal(t, msg, decoded)

	// mismatched encoders and decoders can't read the content produced by each
	encoded, err = Encode(msg, NoopCoder)
	require.NoError(t, err)
	_, err = Decode(encoded, decoder)
	require.Error(t, err)
	require.IsType(t, ErrCannotDecode{}, err)

	encoded, err = Encode(msg, encoder)
	require.NoError(t, err)
	_, err = Decode(encoded, NoopCoder)
	require.Error(t, err)
	require.IsType(t, ErrCannotDecode{}, err)
}

func TestHumanReadable(t *testing.T) {
	// we can produce human readable strings that can then be re-parsed
	key := GenerateSecretKey()
	keyString := HumanReadableKey(key)
	parsedKey, err := ParseHumanReadableKey(keyString)
	require.NoError(t, err)
	require.Equal(t, parsedKey, key)

	// if the prefix is wrong, we can't parse the key
	_, err = ParseHumanReadableKey("A" + keyString)
	require.Error(t, err)

	// With the right prefix, we can't parse if the key isn't base64 encoded
	_, err = ParseHumanReadableKey(humanReadablePrefix + "aaaaa/")
	require.Error(t, err)

	// Extra padding also fails
	_, err = ParseHumanReadableKey(keyString + "=")
	require.Error(t, err)
}
