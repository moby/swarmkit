package namespaces

import "strings"

// Name represents a name within a namespace. It may be any valid DNS
// subdomain.
type Name string

// ParseName parse the name, returning an error if it is not valid.
func ParseName(s string) (Name, error) {
	n := Name(s)
	if err := n.Validate(); err != nil {
		return "", err
	}

	return n, nil
}

// Validate the name, returning an error if not valid.
func (n Name) Validate() error {
	if !isDomainName(n) {
		return ErrNameInvalid
	}

	return nil
}

// Join appends the base to the name, returning an error if the resulting name
// is no longer valid.
func (n Name) Join(base Name) (Name, error) {
	joined := Name(strings.Join([]string{string(n), string(base)}, "."))
	if err := joined.Validate(); err != nil {
		return "", err
	}

	return joined, nil
}

// isDomainName from net/dnsclient.go
func isDomainName(s Name) bool {
	// See RFC 1035, RFC 3696.
	if len(s) == 0 {
		return false
	}
	if len(s) > 255 {
		return false
	}

	last := byte('.')
	ok := false // Ok once we've seen a letter.
	partlen := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		default:
			return false
		case 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || c == '_':
			ok = true
			partlen++
		case '0' <= c && c <= '9':
			// fine
			partlen++
		case c == '-':
			// Byte before dash cannot be dot.
			if last == '.' {
				return false
			}
			partlen++
		case c == '.':
			// Byte before dot cannot be dot, dash.
			if last == '.' || last == '-' {
				return false
			}
			if partlen > 63 || partlen == 0 {
				return false
			}
			partlen = 0
		}
		last = c
	}
	if last == '-' || partlen > 63 {
		return false
	}

	return ok
}
