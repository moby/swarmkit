package encryption

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/docker/swarmkit/api"
	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
)

// This package defines the interfaces and encryption package

const humanReadablePrefix = "SWMKEY-1-"

// ErrCannotDecode is the type of error returned when some data cannot be decoded as plaintext
type ErrCannotDecode struct {
	msg string
}

func (e ErrCannotDecode) Error() string {
	return e.msg
}

// A Decoder can decrypt an encrypted record
type Decoder interface {
	Decode(api.MaybeEncryptedRecord) ([]byte, error)
}

// A Encoder can encrypt some bytes into an encrypted record
type Encoder interface {
	Encode(data []byte) (*api.MaybeEncryptedRecord, error)
}

type noopCoder struct{}

func (n noopCoder) Decode(e api.MaybeEncryptedRecord) ([]byte, error) {
	if e.Algorithm != n.Algorithm() {
		return nil, fmt.Errorf("record is encrypted")
	}
	return e.Data, nil
}

func (n noopCoder) Encode(data []byte) (*api.MaybeEncryptedRecord, error) {
	return &api.MaybeEncryptedRecord{
		Algorithm: n.Algorithm(),
		Data:      data,
	}, nil
}

func (n noopCoder) Algorithm() api.MaybeEncryptedRecord_Algorithm {
	return api.MaybeEncryptedRecord_NotEncrypted
}

// NoopCoder is just a pass-through coder - it does not actually encode or
// decode any data
var NoopCoder = noopCoder{}

// Decode turns a slice of bytes serialized as an MaybeEncryptedRecord into a slice of plaintext bytes
func Decode(encoded []byte, decoder Decoder) ([]byte, error) {
	if decoder == nil {
		return nil, ErrCannotDecode{msg: "no decoder specified"}
	}
	r := api.MaybeEncryptedRecord{}
	if err := proto.Unmarshal(encoded, &r); err != nil {
		// nope, this wasn't marshalled as a MaybeEncryptedRecord
		return nil, ErrCannotDecode{msg: "unable to unmarshal as MaybeEncryptedRecord"}
	}
	plaintext, err := decoder.Decode(r)
	if err != nil {
		return nil, ErrCannotDecode{msg: err.Error()}
	}
	return plaintext, nil
}

// Encode turns a slice of bytes into a serialized MaybeEncryptedRecord slice of bytes
func Encode(plaintext []byte, encoder Encoder) ([]byte, error) {
	if encoder == nil {
		return nil, fmt.Errorf("no encoder specified")
	}

	encryptedRecord, err := encoder.Encode(plaintext)
	if err != nil {
		return nil, errors.Wrap(err, "unable to encode data")
	}

	data, err := proto.Marshal(encryptedRecord)
	if err != nil {
		return nil, errors.Wrap(err, "unable to marshal as MaybeEncryptedRecord")
	}

	return data, nil
}

// Defaults returns a default encoder and decoder
func Defaults(key []byte) (Encoder, Decoder) {
	n := NewNACLSecretbox(key)
	return n, n
}

// GenerateSecretKey generates a secret key that can be used for encrypting data
// using this package
func GenerateSecretKey() []byte {
	secretData := make([]byte, naclSecretboxKeySize)
	if _, err := io.ReadFull(rand.Reader, secretData); err != nil {
		// panic if we can't read random data
		panic(errors.Wrap(err, "failed to read random bytes"))
	}
	return secretData
}

// HumanReadableKey displays a secret key in a human readable way
func HumanReadableKey(key []byte) string {
	// base64-encode the key
	return humanReadablePrefix + base64.StdEncoding.EncodeToString(key)
}

// ParseHumanReadableKey returns a key as bytes from recognized serializations of
// said keys
func ParseHumanReadableKey(key string) ([]byte, error) {
	if !strings.HasPrefix(key, humanReadablePrefix) {
		return nil, fmt.Errorf("invalid key string")
	}
	keyBytes, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(key, humanReadablePrefix))
	if err != nil {
		return nil, fmt.Errorf("invalid key string")
	}
	return keyBytes, nil
}
