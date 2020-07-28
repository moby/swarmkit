package service

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/golang/protobuf/ptypes"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
)

const (
	// Name is the name of the CSI plug-in.
	Name = "mock.gocsi.rexray.com"

	// VendorVersion is the version returned by GetPluginInfo.
	VendorVersion = "1.1.0"
)

// Manifest is the SP's manifest.
var Manifest = map[string]string{
	"url": "https://github.com/rexray/gocsi/tree/master/mock",
}

// Service is the CSI Mock service provider.
type Service interface {
	csi.ControllerServer
	csi.IdentityServer
	csi.NodeServer
}

type service struct {
	sync.Mutex
	nodeID   string
	vols     []csi.Volume
	snaps    []csi.Snapshot
	volsRWL  sync.RWMutex
	snapsRWL sync.RWMutex
	volsNID  uint64
	snapsNID uint64
}

// New returns a new Service.
func New() Service {
	s := &service{nodeID: Name}
	s.vols = []csi.Volume{
		s.newVolume("Mock Volume 1", gib100),
		s.newVolume("Mock Volume 2", gib100),
		s.newVolume("Mock Volume 3", gib100),
	}
	return s
}

const (
	kib    int64 = 1024
	mib    int64 = kib * 1024
	gib    int64 = mib * 1024
	gib100 int64 = gib * 100
	tib    int64 = gib * 1024
	tib100 int64 = tib * 100
)

func (s *service) newVolume(name string, capcity int64) csi.Volume {
	return csi.Volume{
		VolumeId:      fmt.Sprintf("%d", atomic.AddUint64(&s.volsNID, 1)),
		VolumeContext: map[string]string{"name": name},
		CapacityBytes: capcity,
	}
}

func (s *service) newSnapshot(name string, size int64) csi.Snapshot {
	return csi.Snapshot{
		// We set the id to "<volume-id>:<snapshot-id>" since during delete requests
		// we are not given the parent volume id
		SnapshotId:     "12",
		SourceVolumeId: "4",
		SizeBytes:      size,
		CreationTime:   ptypes.TimestampNow(),
		ReadyToUse:     true,
	}
}

func (s *service) findVol(k, v string) (volIdx int, volInfo csi.Volume) {
	s.volsRWL.RLock()
	defer s.volsRWL.RUnlock()
	return s.findVolNoLock(k, v)
}

func (s *service) findVolNoLock(k, v string) (volIdx int, volInfo csi.Volume) {
	volIdx = -1

	for i, vi := range s.vols {
		switch k {
		case "id":
			if strings.EqualFold(v, vi.VolumeId) {
				return i, vi
			}
		case "name":
			if n, ok := vi.VolumeContext["name"]; ok && strings.EqualFold(v, n) {
				return i, vi
			}
		}
	}

	return
}

func (s *service) findVolByName(
	ctx context.Context, name string) (int, csi.Volume) {

	return s.findVol("name", name)
}
