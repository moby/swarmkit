package utils

import (
	"fmt"
	"regexp"
	"strconv"
)

const parseMethodPatt = `^/csi\.v(\d+)\.([^/]+?)/(.+)$`

// ParseMethod parses a gRPC method and returns the CSI version, service
// to which the method belongs, and the method's name. An example value
// for the "fullMethod" argument is "/csi.v0.Identity/GetPluginInfo".
func ParseMethod(
	fullMethod string) (version int32, service, methodName string, err error) {
	rx := regexp.MustCompile(parseMethodPatt)
	m := rx.FindStringSubmatch(fullMethod)
	if len(m) == 0 {
		return 0, "", "", fmt.Errorf("ParseMethod: invalid: %s", fullMethod)
	}
	v, err := strconv.ParseInt(m[1], 10, 32)
	if err != nil {
		return 0, "", "", fmt.Errorf("ParseMethod: %v", err)
	}
	return int32(v), m[2], m[3], nil
}
