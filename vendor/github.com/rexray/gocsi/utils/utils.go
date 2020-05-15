package utils

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

// CSIEndpoint is the name of the environment variable that
// contains the CSI endpoint.
const CSIEndpoint = "CSI_ENDPOINT"

// GetCSIEndpoint returns the network address specified by the
// environment variable CSI_ENDPOINT.
func GetCSIEndpoint() (network, addr string, err error) {
	protoAddr := os.Getenv(CSIEndpoint)
	if emptyRX.MatchString(protoAddr) {
		return "", "", errors.New("missing CSI_ENDPOINT")
	}
	return ParseProtoAddr(protoAddr)
}

// GetCSIEndpointListener returns the net.Listener for the endpoint
// specified by the environment variable CSI_ENDPOINT.
func GetCSIEndpointListener() (net.Listener, error) {
	proto, addr, err := GetCSIEndpoint()
	if err != nil {
		return nil, err
	}
	return net.Listen(proto, addr)
}

const (
	protoAddrGuessPatt = `(?i)^(?:tcp|udp|ip|unix)[^:]*://`

	protoAddrExactPatt = `(?i)^((?:(?:tcp|udp|ip)[46]?)|` +
		`(?:unix(?:gram|packet)?))://(.+)$`
)

var (
	emptyRX          = regexp.MustCompile(`^\s*$`)
	protoAddrGuessRX = regexp.MustCompile(protoAddrGuessPatt)
	protoAddrExactRX = regexp.MustCompile(protoAddrExactPatt)
)

// ErrParseProtoAddrRequired occurs when an empty string is provided
// to ParseProtoAddr.
var ErrParseProtoAddrRequired = errors.New(
	"non-empty network address is required")

// ParseProtoAddr parses a Golang network address.
func ParseProtoAddr(protoAddr string) (proto string, addr string, err error) {

	if emptyRX.MatchString(protoAddr) {
		return "", "", ErrParseProtoAddrRequired
	}

	// If the provided network address does not begin with one
	// of the valid network protocols then treat the string as a
	// file path.
	//
	// First check to see if the file exists at the specified path.
	// If it does then assume it's a valid file path and return it.
	//
	// Otherwise attempt to create the file. If the file can be created
	// without error then remove the file and return the result a UNIX
	// socket file path.
	if !protoAddrGuessRX.MatchString(protoAddr) {

		// If the file already exists then assume it's a valid sock
		// file and return it.
		if _, err := os.Stat(protoAddr); !os.IsNotExist(err) {
			return "unix", protoAddr, nil
		}

		f, err := os.Create(protoAddr)
		if err != nil {
			return "", "", fmt.Errorf(
				"invalid implied sock file: %s: %v", protoAddr, err)
		}
		if err := f.Close(); err != nil {
			return "", "", fmt.Errorf(
				"failed to verify network address as sock file: %s", protoAddr)
		}
		if err := os.RemoveAll(protoAddr); err != nil {
			return "", "", fmt.Errorf(
				"failed to remove verified sock file: %s", protoAddr)
		}
		return "unix", protoAddr, nil
	}

	// Parse the provided network address into the protocol and address parts.
	m := protoAddrExactRX.FindStringSubmatch(protoAddr)
	if m == nil {
		return "", "", fmt.Errorf("invalid network address: %s", protoAddr)
	}
	return m[1], m[2], nil
}

// ParseMap parses a string into a map. The string's expected pattern is:
//
//         KEY1=VAL1, "KEY2=VAL2 ", "KEY 3= VAL3"
//
// The key/value pairs are separated by a comma and optional whitespace.
// Please see the encoding/csv package (https://goo.gl/1j1xb9) for information
// on how to quote keys and/or values to include leading and trailing
// whitespace.
func ParseMap(line string) map[string]string {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	r := csv.NewReader(strings.NewReader(line))
	r.TrimLeadingSpace = true

	record, err := r.Read()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		panic(err)
	}

	data := map[string]string{}
	for i := range record {
		p := strings.SplitN(record[i], "=", 2)
		if len(p) == 0 {
			continue
		}
		k := p[0]
		var v string
		if len(p) > 1 {
			v = p[1]
		}
		data[k] = v
	}

	return data
}

// ParseSlice parses a string into a slice. The string's expected pattern is:
//
//         VAL1, "VAL2 ", " VAL3 "
//
// The values are separated by a comma and optional whitespace. Please see
// the encoding/csv package (https://goo.gl/1j1xb9) for information on how to
// quote values to include leading and trailing whitespace.
func ParseSlice(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	r := csv.NewReader(strings.NewReader(line))
	r.TrimLeadingSpace = true

	record, err := r.Read()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		panic(err)
	}

	return record
}

// ParseMapWS parses a string into a map. The string's expected pattern is:
//
//         KEY1=VAL1 KEY2="VAL2 " "KEY 3"=' VAL3'
//
// The key/value pairs are separated by one or more whitespace characters.
// Keys and/or values with whitespace should be quoted with either single
// or double quotes.
func ParseMapWS(line string) map[string]string {
	if line == "" {
		return nil
	}

	var (
		escp bool
		quot rune
		ckey string
		keyb = &bytes.Buffer{}
		valb = &bytes.Buffer{}
		word = keyb
		data = map[string]string{}
	)

	for i, c := range line {
		// Check to see if the character is a quote character.
		switch c {
		case '\\':
			// If not already escaped then activate escape.
			if !escp {
				escp = true
				continue
			}
		case '\'', '"':
			// If the quote or double quote is the first char or
			// an unescaped char then determine if this is the
			// beginning of a quote or end of one.
			if i == 0 || !escp {
				if quot == c {
					quot = 0
				} else {
					quot = c
				}
				continue
			}
		case '=':
			// If the word buffer is currently the key buffer,
			// quoting is not enabled, and the preceeding character
			// is not the escape character then the equal sign indicates
			// a transition from key to value.
			if word == keyb && quot == 0 && !escp {
				ckey = keyb.String()
				keyb.Reset()
				word = valb
				continue
			}
		case ' ', '\t':
			// If quoting is not enabled and the preceeding character is
			// not the escape character then record the value into the
			// map and fast-forward the cursor to the next, non-whitespace
			// character.
			if quot == 0 && !escp {
				// Record the value into the map for the current key.
				if ckey != "" {
					data[ckey] = valb.String()
					valb.Reset()
					word = keyb
					ckey = ""
				}
				continue
			}
		}
		if escp {
			escp = false
		}
		word.WriteRune(c)
	}

	// If the current key string is not empty then record it with the value
	// buffer's string value as a new pair.
	if ckey != "" {
		data[ckey] = valb.String()
	}

	return data
}

// NewMountCapability returns a new *csi.VolumeCapability for a
// volume that is to be mounted.
func NewMountCapability(
	mode csi.VolumeCapability_AccessMode_Mode,
	fsType string,
	mountFlags ...string) *csi.VolumeCapability {

	return &csi.VolumeCapability{
		AccessMode: &csi.VolumeCapability_AccessMode{
			Mode: mode,
		},
		AccessType: &csi.VolumeCapability_Mount{
			Mount: &csi.VolumeCapability_MountVolume{
				FsType:     fsType,
				MountFlags: mountFlags,
			},
		},
	}
}

// NewBlockCapability returns a new *csi.VolumeCapability for a
// volume that is to be accessed as a raw device.
func NewBlockCapability(
	mode csi.VolumeCapability_AccessMode_Mode) *csi.VolumeCapability {

	return &csi.VolumeCapability{
		AccessMode: &csi.VolumeCapability_AccessMode{
			Mode: mode,
		},
		AccessType: &csi.VolumeCapability_Block{
			Block: &csi.VolumeCapability_BlockVolume{},
		},
	}
}

// PageVolumes issues one or more ListVolumes requests to retrieve
// all available volumes, returning them over a Go channel.
func PageVolumes(
	ctx context.Context,
	client csi.ControllerClient,
	req csi.ListVolumesRequest,
	opts ...grpc.CallOption) (<-chan csi.Volume, <-chan error) {

	var (
		cvol = make(chan csi.Volume)
		cerr = make(chan error)
	)

	// Execute the RPC in a goroutine, looping until there are no
	// more volumes available.
	go func() {
		var (
			wg     sync.WaitGroup
			pages  int
			cancel context.CancelFunc
		)

		// Get a cancellation context used to control the interaction
		// between returning volumes and the possibility of an error.
		ctx, cancel = context.WithCancel(ctx)

		// waitAndClose closes the volume and error channels after all
		// channel-dependent goroutines have completed their work
		defer func() {
			wg.Wait()
			close(cerr)
			close(cvol)
			log.WithField("pages", pages).Debug("PageAllVolumes: exit")
		}()

		sendVolumes := func(res csi.ListVolumesResponse) {
			// Loop over the volume entries until they're all gone
			// or the context is cancelled.
			var i int
			for i = 0; i < len(res.Entries) && ctx.Err() == nil; i++ {

				// Send the volume over the channel.
				cvol <- *res.Entries[i].Volume

				// Let the wait group know that this worker has completed
				// its task.
				wg.Done()
			}
			// If not all volumes have been sent over the channel then
			// deduct the remaining number from the wait group.
			if i != len(res.Entries) {
				rem := len(res.Entries) - i
				log.WithFields(map[string]interface{}{
					"cancel":    ctx.Err(),
					"remaining": rem,
				}).Warn("PageAllVolumes: cancelled w unprocessed results")
				wg.Add(-rem)
			}
		}

		// listVolumes returns true if there are more volumes to list.
		listVolumes := func() bool {

			// The wait group "wg" is blocked during the execution of
			// this function.
			wg.Add(1)
			defer wg.Done()

			res, err := client.ListVolumes(ctx, &req, opts...)
			if err != nil {
				cerr <- err

				// Invoke the cancellation context function to
				// ensure that work wraps up as quickly as possible.
				cancel()

				return false
			}

			// Add to the number of workers
			wg.Add(len(res.Entries))

			// Process the retrieved volumes.
			go sendVolumes(*res)

			// Set the request's starting token to the response's
			// next token.
			req.StartingToken = res.NextToken
			return req.StartingToken != ""
		}

		// List volumes until there are no more volumes or the context
		// is cancelled.
		for {
			if ctx.Err() != nil {
				break
			}
			if !listVolumes() {
				break
			}
			pages++
		}
	}()

	return cvol, cerr
}

// PageSnapshots issues one or more ListSnapshots requests to retrieve
// all available snaphsots, returning them over a Go channel.
func PageSnapshots(
	ctx context.Context,
	client csi.ControllerClient,
	req csi.ListSnapshotsRequest,
	opts ...grpc.CallOption) (<-chan csi.Snapshot, <-chan error) {

	var (
		csnap = make(chan csi.Snapshot)
		cerr  = make(chan error)
	)

	// Execute the RPC in a goroutine, looping until there are no
	// more snaphsots available.
	go func() {
		var (
			wg     sync.WaitGroup
			pages  int
			cancel context.CancelFunc
		)

		// Get a cancellation context used to control the interaction
		// between returning snaphsots and the possibility of an error.
		ctx, cancel = context.WithCancel(ctx)

		// waitAndClose closes the snaphsot and error channels after all
		// channel-dependent goroutines have completed their work
		defer func() {
			wg.Wait()
			close(cerr)
			close(csnap)
			log.WithField("pages", pages).Debug("PageAllSnapshots: exit")
		}()

		sendSnapshots := func(res csi.ListSnapshotsResponse) {
			// Loop over the snaphsot entries until they're all gone
			// or the context is cancelled.
			var i int
			for i = 0; i < len(res.Entries) && ctx.Err() == nil; i++ {

				// Send the snaphsot over the channel.
				csnap <- *res.Entries[i].Snapshot

				// Let the wait group know that this worker has completed
				// its task.
				wg.Done()
			}
			// If not all snaphsots have been sent over the channel then
			// deduct the remaining number from the wait group.
			if i != len(res.Entries) {
				rem := len(res.Entries) - i
				log.WithFields(map[string]interface{}{
					"cancel":    ctx.Err(),
					"remaining": rem,
				}).Warn("PageAllSnapshots: cancelled w unprocessed results")
				wg.Add(-rem)
			}
		}

		// listSnapshots returns true if there are more snaphsots to list.
		listSnapshots := func() bool {

			// The wait group "wg" is blocked during the execution of
			// this function.
			wg.Add(1)
			defer wg.Done()

			res, err := client.ListSnapshots(ctx, &req, opts...)
			if err != nil {
				cerr <- err

				// Invoke the cancellation context function to
				// ensure that work wraps up as quickly as possible.
				cancel()

				return false
			}

			// Add to the number of workers
			wg.Add(len(res.Entries))

			// Process the retrieved snaphsots.
			go sendSnapshots(*res)

			// Set the request's starting token to the response's
			// next token.
			req.StartingToken = res.NextToken
			return req.StartingToken != ""
		}

		// List snaphsots until there are no more snaphsots or the context
		// is cancelled.
		for {
			if ctx.Err() != nil {
				break
			}
			if !listSnapshots() {
				break
			}
			pages++
		}
	}()

	return csnap, cerr
}

// IsSuccess returns nil if the provided error is an RPC error with an error
// code that is OK (0) or matches one of the additional, provided successful
// error codes. Otherwise the original error is returned.
func IsSuccess(err error, successCodes ...codes.Code) error {

	// Shortcut the process by first checking to see if the error is nil.
	if err == nil {
		return nil
	}

	// Check to see if the provided error is an RPC error.
	stat, ok := status.FromError(err)
	if !ok {
		return err
	}

	if stat.Code() == codes.OK {
		return nil
	}
	for _, c := range successCodes {
		if stat.Code() == c {
			return nil
		}
	}

	return err
}

// AreVolumeCapabilitiesCompatible returns a flag indicating whether
// the volume capability array "a" is compatible with "b". A true value
// indicates that "a" and "b" are equivalent or "b" is a superset of "a".
func AreVolumeCapabilitiesCompatible(
	a, b []*csi.VolumeCapability) (bool, error) {

	if len(a) > len(b) {
		return false, status.Error(
			codes.AlreadyExists,
			"requested capabilities exceed existing")
	}

	var i int

	for _, va := range a {
		for _, vb := range b {
			if EqualVolumeCapability(va, vb) {
				i++
			}
		}
	}

	return i >= len(a), nil
}

// IsVolumeCapabilityCompatible returns a flag indicating whether
// the volume capability "a" is compatible with the set "b". A true value
// indicates that "a" and "b" are equivalent or "b" is a superset of "a".
func IsVolumeCapabilityCompatible(
	a *csi.VolumeCapability, b []*csi.VolumeCapability) (bool, error) {

	return AreVolumeCapabilitiesCompatible([]*csi.VolumeCapability{a}, b)
}

// EqualVolumeCapability returns a flag indicating if two csi.VolumeCapability
// objects are equal. If a and b are both nil then false is returned.
func EqualVolumeCapability(a, b *csi.VolumeCapability) bool {
	if a == nil || b == nil {
		return false
	}

	// Compare access modes.
	if a.AccessMode != nil && b.AccessMode == nil {
		return false
	}
	if a.AccessMode == nil && b.AccessMode != nil {
		return false
	}
	if a.AccessMode != nil && b.AccessMode != nil &&
		a.AccessMode.Mode != b.AccessMode.Mode {
		return false
	}

	// If both capabilities are block then return true.
	if a.GetBlock() != nil && b.GetBlock() != nil {
		return true
	}

	aMount := a.GetMount()
	bMount := b.GetMount()
	if aMount != nil && bMount != nil {

		// If the filesystem types are incompatible then return false.
		if aMount.FsType != bMount.FsType {
			return false
		}

		// Compare the mount flags lengths.
		if len(aMount.MountFlags) != len(bMount.MountFlags) {
			return false
		}

		// Copy the mount flags to prevent the original order
		// from changing due to the sort operation below.
		af := append([]string{}, aMount.MountFlags...)
		bf := append([]string{}, bMount.MountFlags...)

		// Sort the mount flags prior to comparison.
		sort.Strings(af)
		sort.Strings(bf)

		// Compare the mount flags.
		for j := range af {
			if af[j] != bf[j] {
				return false
			}
		}

		// The mount capabilities are compatible; return true.
		return true
	}

	return false
}

// EqualVolume returns a flag indicating if two csi.Volume
// objects are equal. If a and b are both nil then false is returned.
func EqualVolume(a, b *csi.Volume) bool {
	if a == nil || b == nil {
		return false
	}
	return CompareVolume(*a, *b) == 0
}

// CompareVolume compares two csi.Volume objects and returns a
// negative number if a < b, a positive number if a > b, and zero if
// a == b.
func CompareVolume(a, b csi.Volume) int {
	if a.VolumeId < b.VolumeId {
		return -1
	}
	if a.VolumeId > b.VolumeId {
		return 1
	}
	if a.CapacityBytes < b.CapacityBytes {
		return -1
	}
	if a.CapacityBytes > b.CapacityBytes {
		return 1
	}
	if len(a.VolumeContext) < len(b.VolumeContext) {
		return -1
	}
	if len(a.VolumeContext) > len(b.VolumeContext) {
		return 1
	}
	for k, v := range a.VolumeContext {
		if v < b.VolumeContext[k] {
			return -1
		}
		if v > b.VolumeContext[k] {
			return 1
		}
	}
	return 0
}
