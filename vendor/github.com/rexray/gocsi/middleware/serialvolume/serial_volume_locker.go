package serialvolume

import (
	"context"
	"io"
	"time"

	"github.com/akutz/gosync"
	"github.com/container-storage-interface/spec/lib/go/csi"
	xctx "golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mwtypes "github.com/rexray/gocsi/middleware/serialvolume/types"
)

const pending = "pending"

// Option configures the interceptor.
type Option func(*opts)

type opts struct {
	timeout time.Duration
	locker  mwtypes.VolumeLockerProvider
}

// WithTimeout is an Option that sets the timeout used by the interceptor.
func WithTimeout(t time.Duration) Option {
	return func(o *opts) {
		o.timeout = t
	}
}

// WithLockProvider is an Option that sets the lock provider used by the
// interceptor.
func WithLockProvider(p mwtypes.VolumeLockerProvider) Option {
	return func(o *opts) {
		o.locker = p
	}
}

// New returns a new server-side, gRPC interceptor
// that provides serial access to volume resources across the following
// RPCs:
//
//  * CreateVolume
//  * DeleteVolume
//  * ControllerPublishVolume
//  * ControllerUnpublishVolume
//  * NodePublishVolume
//  * NodeUnpublishVolume
func New(opts ...Option) grpc.UnaryServerInterceptor {

	i := &interceptor{}

	// Configure the interceptor's options.
	for _, setOpt := range opts {
		setOpt(&i.opts)
	}

	// If no lock provider is configured then set the default,
	// in-memory provider.
	if i.opts.locker == nil {
		i.opts.locker = &defaultLockProvider{
			volIDLocks:   map[string]gosync.TryLocker{},
			volNameLocks: map[string]gosync.TryLocker{},
		}
	}

	return i.handle
}

type interceptor struct {
	opts opts
}

func (i *interceptor) handle(
	ctx xctx.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (interface{}, error) {

	switch treq := req.(type) {
	case *csi.ControllerPublishVolumeRequest:
		return i.controllerPublishVolume(ctx, treq, info, handler)
	case *csi.ControllerUnpublishVolumeRequest:
		return i.controllerUnpublishVolume(ctx, treq, info, handler)
	case *csi.CreateVolumeRequest:
		return i.createVolume(ctx, treq, info, handler)
	case *csi.DeleteVolumeRequest:
		return i.deleteVolume(ctx, treq, info, handler)
	case *csi.NodePublishVolumeRequest:
		return i.nodePublishVolume(ctx, treq, info, handler)
	case *csi.NodeUnpublishVolumeRequest:
		return i.nodeUnpublishVolume(ctx, treq, info, handler)
	}

	return handler(ctx, req)
}

func (i *interceptor) controllerPublishVolume(
	ctx context.Context,
	req *csi.ControllerPublishVolumeRequest,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (res interface{}, resErr error) {

	lock, err := i.opts.locker.GetLockWithID(ctx, req.VolumeId)
	if err != nil {
		return nil, err
	}
	if closer, ok := lock.(io.Closer); ok {
		defer closer.Close()
	}
	if !lock.TryLock(i.opts.timeout) {
		return nil, status.Error(codes.Aborted, pending)
	}
	defer lock.Unlock()

	return handler(ctx, req)
}

func (i *interceptor) controllerUnpublishVolume(
	ctx context.Context,
	req *csi.ControllerUnpublishVolumeRequest,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (res interface{}, resErr error) {

	lock, err := i.opts.locker.GetLockWithID(ctx, req.VolumeId)
	if err != nil {
		return nil, err
	}
	if closer, ok := lock.(io.Closer); ok {
		defer closer.Close()
	}
	if !lock.TryLock(i.opts.timeout) {
		return nil, status.Error(codes.Aborted, pending)
	}
	defer lock.Unlock()

	return handler(ctx, req)
}

func (i *interceptor) createVolume(
	ctx context.Context,
	req *csi.CreateVolumeRequest,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (res interface{}, resErr error) {

	lock, err := i.opts.locker.GetLockWithName(ctx, req.Name)
	if err != nil {
		return nil, err
	}
	if closer, ok := lock.(io.Closer); ok {
		defer closer.Close()
	}
	if !lock.TryLock(i.opts.timeout) {
		return nil, status.Error(codes.Aborted, pending)
	}
	defer lock.Unlock()

	return handler(ctx, req)
}

func (i *interceptor) deleteVolume(
	ctx context.Context,
	req *csi.DeleteVolumeRequest,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (res interface{}, resErr error) {

	lock, err := i.opts.locker.GetLockWithID(ctx, req.VolumeId)
	if err != nil {
		return nil, err
	}
	if closer, ok := lock.(io.Closer); ok {
		defer closer.Close()
	}
	if !lock.TryLock(i.opts.timeout) {
		return nil, status.Error(codes.Aborted, pending)
	}
	defer lock.Unlock()

	return handler(ctx, req)
}

func (i *interceptor) nodePublishVolume(
	ctx context.Context,
	req *csi.NodePublishVolumeRequest,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (res interface{}, resErr error) {

	lock, err := i.opts.locker.GetLockWithID(ctx, req.VolumeId)
	if err != nil {
		return nil, err
	}
	if closer, ok := lock.(io.Closer); ok {
		defer closer.Close()
	}
	if !lock.TryLock(i.opts.timeout) {
		return nil, status.Error(codes.Aborted, pending)
	}
	defer lock.Unlock()

	return handler(ctx, req)
}

func (i *interceptor) nodeUnpublishVolume(
	ctx context.Context,
	req *csi.NodeUnpublishVolumeRequest,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (res interface{}, resErr error) {

	lock, err := i.opts.locker.GetLockWithID(ctx, req.VolumeId)
	if err != nil {
		return nil, err
	}
	if closer, ok := lock.(io.Closer); ok {
		defer closer.Close()
	}
	if !lock.TryLock(i.opts.timeout) {
		return nil, status.Error(codes.Aborted, pending)
	}
	defer lock.Unlock()

	return handler(ctx, req)
}
