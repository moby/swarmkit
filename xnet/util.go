package xnet

import (
	"fmt"
	"strings"

	"github.com/Sirupsen/logrus"
)

// ParseProtoAddr parses an address as a protcol and address pair.
// If no protocol specified, this function return an error
func ParseProtoAddr(s string) (string, string, error) {
	logrus.Debugf("parse proto addr: %s", s)
	parts := strings.SplitN(s, "://", 2)
	if len(parts) == 1 {
		return "", "", fmt.Errorf("No protocol is specified in '%s'", s)
	}
	return parts[0], parts[1], nil
}
