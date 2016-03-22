package namespaces

import "errors"

var (
	ErrNameInvalid    = errors.New("namespaces: invalid name")
	ErrNameUnresolved = errors.New("namespaces: unresolvable name")
)
