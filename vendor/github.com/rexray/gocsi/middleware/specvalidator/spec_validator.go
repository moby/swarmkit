package specvalidator

import (
	"reflect"
	"regexp"
	"sync"

	"github.com/golang/protobuf/proto"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"github.com/rexray/gocsi/utils"
)

// Option configures the spec validator interceptor.
type Option func(*opts)

type opts struct {
	sync.Mutex
	reqValidation               bool
	repValidation               bool
	requiresStagingTargetPath   bool
	requiresVolContext          bool
	requiresPubContext          bool
	requiresCtlrNewVolSecrets   bool
	requiresCtlrDelVolSecrets   bool
	requiresCtlrPubVolSecrets   bool
	requiresCtlrUnpubVolSecrets bool
	requiresNodeStgVolSecrets   bool
	requiresNodePubVolSecrets   bool
	disableFieldLenCheck        bool
}

// WithRequestValidation is a Option that enables request validation.
func WithRequestValidation() Option {
	return func(o *opts) {
		o.reqValidation = true
	}
}

// WithResponseValidation is a Option that enables response validation.
func WithResponseValidation() Option {
	return func(o *opts) {
		o.repValidation = true
	}
}

// WithRequiresStagingTargetPath is a Option that indicates
// NodePublishVolume requests must have non-empty StagingTargetPath
// fields.
func WithRequiresStagingTargetPath() Option {
	return func(o *opts) {
		o.requiresStagingTargetPath = true
	}
}

// WithRequiresVolumeContext is a Option that indicates
// ControllerPublishVolume requests, ValidateVolumeCapabilities requests,
// NodeStageVolume requests, and NodePublishVolume requests must contain
// non-empty publish volume context data.
func WithRequiresVolumeContext() Option {
	return func(o *opts) {
		o.requiresVolContext = true
	}
}

// WithRequiresPublishContext is a Option that indicates
// ControllerPublishVolume responses, NodePublishVolume requests, and
// NodeStageVolume requests must contain non-empty publish volume context data.
func WithRequiresPublishContext() Option {
	return func(o *opts) {
		o.requiresPubContext = true
	}
}

// WithRequiresControllerCreateVolumeSecrets is a Option
// that indicates the eponymous requests must contain non-empty secrets
// data.
func WithRequiresControllerCreateVolumeSecrets() Option {
	return func(o *opts) {
		o.requiresCtlrNewVolSecrets = true
	}
}

// WithRequiresControllerDeleteVolumeSecrets is a Option
// that indicates the eponymous requests must contain non-empty credentials
// data.
func WithRequiresControllerDeleteVolumeSecrets() Option {
	return func(o *opts) {
		o.requiresCtlrDelVolSecrets = true
	}
}

// WithRequiresControllerPublishVolumeSecrets is a Option
// that indicates the eponymous requests must contain non-empty credentials
// data.
func WithRequiresControllerPublishVolumeSecrets() Option {
	return func(o *opts) {
		o.requiresCtlrPubVolSecrets = true
	}
}

// WithRequiresControllerUnpublishVolumeSecrets is a Option
// that indicates the eponymous requests must contain non-empty credentials
// data.
func WithRequiresControllerUnpublishVolumeSecrets() Option {
	return func(o *opts) {
		o.requiresCtlrUnpubVolSecrets = true
	}
}

// WithRequiresNodeStageVolumeSecrets is a Option
// that indicates the eponymous requests must contain non-empty credentials
// data.
func WithRequiresNodeStageVolumeSecrets() Option {
	return func(o *opts) {
		o.requiresNodeStgVolSecrets = true
	}
}

// WithRequiresNodePublishVolumeSecrets is a Option
// that indicates the eponymous requests must contain non-empty credentials
// data.
func WithRequiresNodePublishVolumeSecrets() Option {
	return func(o *opts) {
		o.requiresNodePubVolSecrets = true
	}
}

// WithDisableFieldLenCheck is a Option
// that indicates that the length of fields should not be validated
func WithDisableFieldLenCheck() Option {
	return func(o *opts) {
		o.disableFieldLenCheck = true
	}
}

type interceptor struct {
	opts opts
}

// NewServerSpecValidator returns a new UnaryServerInterceptor that validates
// server request and response data against the CSI specification.
func NewServerSpecValidator(
	opts ...Option) grpc.UnaryServerInterceptor {

	return newSpecValidator(opts...).handleServer
}

// NewClientSpecValidator provides a UnaryClientInterceptor that validates
// client request and response data against the CSI specification.
func NewClientSpecValidator(
	opts ...Option) grpc.UnaryClientInterceptor {

	return newSpecValidator(opts...).handleClient
}

func newSpecValidator(opts ...Option) *interceptor {
	i := &interceptor{}
	for _, withOpts := range opts {
		withOpts(&i.opts)
	}
	return i
}

func (s *interceptor) handleServer(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (interface{}, error) {

	return s.handle(ctx, info.FullMethod, req, func() (interface{}, error) {
		return handler(ctx, req)
	})
}

func (s *interceptor) handleClient(
	ctx context.Context,
	method string,
	req, rep interface{},
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption) error {

	_, err := s.handle(ctx, method, req, func() (interface{}, error) {
		return rep, invoker(ctx, method, req, rep, cc, opts...)
	})
	return err
}

func (s *interceptor) handle(
	ctx context.Context,
	method string,
	req interface{},
	next func() (interface{}, error)) (interface{}, error) {

	// If the request is nil then pass control to the next handler
	// in the chain.
	if req == nil {
		return next()
	}

	if s.opts.reqValidation {
		// Validate the request against the CSI specification.
		if err := s.validateRequest(ctx, method, req); err != nil {
			return nil, err
		}
	}

	// Use the function passed into this one to get the response. On the
	// server-side this could possibly invoke additional interceptors or
	// the RPC. On the client side this invokes the RPC.
	rep, err := next()

	if err != nil {
		return nil, err
	}

	if s.opts.repValidation {
		log.Debug("response validation enabled")
		// Validate the response against the CSI specification.
		if err := s.validateResponse(ctx, method, rep); err != nil {

			// If an error occurred while validating the response, it is
			// imperative the response not be discarded as it could be
			// important to the client.
			st, ok := status.FromError(err)
			if !ok {
				st = status.New(codes.Internal, err.Error())
			}

			// Add the response to the error details.
			st, err2 := st.WithDetails(rep.(proto.Message))

			// If there is a problem encoding the response into the
			// protobuf details then err on the side of caution, log
			// the encoding error, validation error, and return the
			// original response.
			if err2 != nil {
				log.WithFields(map[string]interface{}{
					"encErr": err2,
					"valErr": err,
				}).Error("failed to encode error details; " +
					"returning invalid response")

				return rep, nil
			}

			// There was no issue encoding the response, so return
			// the gRPC status error with the error message and payload.
			return nil, st.Err()
		}
	}

	return rep, err
}

type interceptorHasVolumeID interface {
	GetVolumeId() string
}
type interceptorHasUserCredentials interface {
	GetUserCredentials() map[string]string
}

type interceptorHasVolumeContext interface {
	GetVolumeContext() map[string]string
}

type interceptorHasPublishContext interface {
	GetPublishContext() map[string]string
}

func (s *interceptor) validateRequest(
	ctx context.Context,
	method string,
	req interface{}) error {

	if req == nil {
		return nil
	}

	// Validate field sizes.
	if !s.opts.disableFieldLenCheck {
		if err := validateFieldSizes(req); err != nil {
			return err
		}
	}

	// Check to see if the request has a volume ID and if it is set.
	// If the volume ID is not set then return an error.
	if treq, ok := req.(interceptorHasVolumeID); ok {
		if treq.GetVolumeId() == "" {
			return status.Error(
				codes.InvalidArgument, "required: VolumeID")
		}
	}

	// Check to see if the request has volume context and if they're
	// required. If the volume context is required by no attributes are
	// specified then return an error.
	if s.opts.requiresVolContext {
		if treq, ok := req.(interceptorHasVolumeContext); ok {
			if len(treq.GetVolumeContext()) == 0 {
				return status.Error(
					codes.InvalidArgument, "required: VolumeContext")
			}
		}
	}

	// Check to see if the request has publish context and if they're
	// required. If the publish context is required by no attributes are
	// specified then return an error.
	if s.opts.requiresPubContext {
		if treq, ok := req.(interceptorHasPublishContext); ok {
			if len(treq.GetPublishContext()) == 0 {
				return status.Error(
					codes.InvalidArgument, "required: PublishContext")
			}
		}
	}

	// Please leave requests that do not require explicit validation commented
	// out for purposes of optimization. These requests are retained in this
	// form to make it easy to add validation later if required.
	//
	switch tobj := req.(type) {
	//
	// Controller Service
	//
	case *csi.CreateVolumeRequest:
		return s.validateCreateVolumeRequest(ctx, *tobj)
	case *csi.DeleteVolumeRequest:
		return s.validateDeleteVolumeRequest(ctx, *tobj)
	case *csi.ControllerPublishVolumeRequest:
		return s.validateControllerPublishVolumeRequest(ctx, *tobj)
	case *csi.ControllerUnpublishVolumeRequest:
		return s.validateControllerUnpublishVolumeRequest(ctx, *tobj)
	case *csi.ValidateVolumeCapabilitiesRequest:
		return s.validateValidateVolumeCapabilitiesRequest(ctx, *tobj)
	case *csi.GetCapacityRequest:
		return s.validateGetCapacityRequest(ctx, *tobj)
		//
		// Node Service
		//
	case *csi.NodeStageVolumeRequest:
		return s.validateNodeStageVolumeRequest(ctx, *tobj)
	case *csi.NodeUnstageVolumeRequest:
		return s.validateNodeUnstageVolumeRequest(ctx, *tobj)
	case *csi.NodePublishVolumeRequest:
		return s.validateNodePublishVolumeRequest(ctx, *tobj)
	case *csi.NodeUnpublishVolumeRequest:
		return s.validateNodeUnpublishVolumeRequest(ctx, *tobj)
	}

	return nil
}

func (s *interceptor) validateResponse(
	ctx context.Context,
	method string,
	rep interface{}) error {

	if utils.IsNilResponse(rep) {
		return status.Error(codes.Internal, "nil response")
	}

	// Validate the field sizes.
	if !s.opts.disableFieldLenCheck {
		if err := validateFieldSizes(rep); err != nil {
			return err
		}
	}

	switch tobj := rep.(type) {
	//
	// Controller Service
	//
	case *csi.CreateVolumeResponse:
		return s.validateCreateVolumeResponse(ctx, *tobj)
	case *csi.ControllerPublishVolumeResponse:
		return s.validateControllerPublishVolumeResponse(ctx, *tobj)
	case *csi.ListVolumesResponse:
		return s.validateListVolumesResponse(ctx, *tobj)
	case *csi.ControllerGetCapabilitiesResponse:
		return s.validateControllerGetCapabilitiesResponse(ctx, *tobj)
	//
	// Identity Service
	//
	case *csi.GetPluginInfoResponse:
		return s.validateGetPluginInfoResponse(ctx, *tobj)
	//
	// Node Service
	//
	case *csi.NodeGetInfoResponse:
		return s.validateNodeGetInfoResponse(ctx, *tobj)
	case *csi.NodeGetCapabilitiesResponse:
		return s.validateNodeGetCapabilitiesResponse(ctx, *tobj)
	}

	return nil
}

func (s *interceptor) validateCreateVolumeRequest(
	ctx context.Context,
	req csi.CreateVolumeRequest) error {

	if req.Name == "" {
		return status.Error(
			codes.InvalidArgument, "required: Name")
	}
	if s.opts.requiresCtlrNewVolSecrets {
		if len(req.Secrets) == 0 {
			return status.Error(
				codes.InvalidArgument, "required: Secrets")
		}
	}

	return validateVolumeCapabilitiesArg(req.VolumeCapabilities, true)
}

func (s *interceptor) validateDeleteVolumeRequest(
	ctx context.Context,
	req csi.DeleteVolumeRequest) error {

	if s.opts.requiresCtlrDelVolSecrets {
		if len(req.Secrets) == 0 {
			return status.Error(
				codes.InvalidArgument, "required: Secrets")
		}
	}

	return nil
}

func (s *interceptor) validateControllerPublishVolumeRequest(
	ctx context.Context,
	req csi.ControllerPublishVolumeRequest) error {

	if s.opts.requiresCtlrPubVolSecrets {
		if len(req.Secrets) == 0 {
			return status.Error(
				codes.InvalidArgument, "required: Secrets")
		}
	}

	if req.NodeId == "" {
		return status.Error(
			codes.InvalidArgument, "required: NodeID")
	}

	return validateVolumeCapabilityArg(req.VolumeCapability, true)
}

func (s *interceptor) validateControllerUnpublishVolumeRequest(
	ctx context.Context,
	req csi.ControllerUnpublishVolumeRequest) error {

	if s.opts.requiresCtlrUnpubVolSecrets {
		if len(req.Secrets) == 0 {
			return status.Error(
				codes.InvalidArgument, "required: Secrets")
		}
	}

	return nil
}

func (s *interceptor) validateValidateVolumeCapabilitiesRequest(
	ctx context.Context,
	req csi.ValidateVolumeCapabilitiesRequest) error {

	return validateVolumeCapabilitiesArg(req.VolumeCapabilities, true)
}

func (s *interceptor) validateGetCapacityRequest(
	ctx context.Context,
	req csi.GetCapacityRequest) error {

	return validateVolumeCapabilitiesArg(req.VolumeCapabilities, false)
}

func (s *interceptor) validateNodeStageVolumeRequest(
	ctx context.Context,
	req csi.NodeStageVolumeRequest) error {

	if req.StagingTargetPath == "" {
		return status.Error(
			codes.InvalidArgument, "required: StagingTargetPath")
	}

	if s.opts.requiresNodeStgVolSecrets {
		if len(req.Secrets) == 0 {
			return status.Error(
				codes.InvalidArgument, "required: Secrets")
		}
	}

	return validateVolumeCapabilityArg(req.VolumeCapability, true)
}

func (s *interceptor) validateNodeUnstageVolumeRequest(
	ctx context.Context,
	req csi.NodeUnstageVolumeRequest) error {

	if req.StagingTargetPath == "" {
		return status.Error(
			codes.InvalidArgument, "required: StagingTargetPath")
	}

	return nil
}

func (s *interceptor) validateNodePublishVolumeRequest(
	ctx context.Context,
	req csi.NodePublishVolumeRequest) error {

	if s.opts.requiresStagingTargetPath && req.StagingTargetPath == "" {
		return status.Error(
			codes.InvalidArgument, "required: StagingTargetPath")
	}

	if req.TargetPath == "" {
		return status.Error(
			codes.InvalidArgument, "required: TargetPath")
	}

	if s.opts.requiresNodePubVolSecrets {
		if len(req.Secrets) == 0 {
			return status.Error(
				codes.InvalidArgument, "required: Secrets")
		}
	}

	return validateVolumeCapabilityArg(req.VolumeCapability, true)
}

func (s *interceptor) validateNodeUnpublishVolumeRequest(
	ctx context.Context,
	req csi.NodeUnpublishVolumeRequest) error {

	if req.TargetPath == "" {
		return status.Error(
			codes.InvalidArgument, "required: TargetPath")
	}

	return nil
}

func (s *interceptor) validateCreateVolumeResponse(
	ctx context.Context,
	rep csi.CreateVolumeResponse) error {

	if rep.Volume == nil {
		return status.Error(codes.Internal, "nil: Volume")
	}

	if rep.Volume.VolumeId == "" {
		return status.Error(codes.Internal, "empty: Volume.Id")
	}

	if s.opts.requiresVolContext && len(rep.Volume.VolumeContext) == 0 {
		return status.Error(
			codes.Internal, "non-nil, empty: Volume.VolumeContext")
	}

	return nil
}

func (s *interceptor) validateControllerPublishVolumeResponse(
	ctx context.Context,
	rep csi.ControllerPublishVolumeResponse) error {

	if s.opts.requiresPubContext && len(rep.PublishContext) == 0 {
		return status.Error(codes.Internal, "empty: PublishContext")
	}
	return nil
}

func (s *interceptor) validateListVolumesResponse(
	ctx context.Context,
	rep csi.ListVolumesResponse) error {

	for i, e := range rep.Entries {
		vol := e.Volume
		if vol == nil {
			return status.Errorf(
				codes.Internal,
				"nil: Entries[%d].Volume", i)
		}
		if vol.VolumeId == "" {
			return status.Errorf(
				codes.Internal,
				"empty: Entries[%d].Volume.Id", i)
		}
		if vol.VolumeContext != nil && len(vol.VolumeContext) == 0 {
			return status.Errorf(
				codes.Internal,
				"non-nil, empty: Entries[%d].Volume.VolumeContext", i)
		}
	}

	return nil
}

func (s *interceptor) validateControllerGetCapabilitiesResponse(
	ctx context.Context,
	rep csi.ControllerGetCapabilitiesResponse) error {

	if rep.Capabilities != nil && len(rep.Capabilities) == 0 {
		return status.Error(codes.Internal, "non-nil, empty: Capabilities")
	}
	return nil
}

const (
	pluginNameMax           = 63
	pluginNamePatt          = `^[\w\d]+\.[\w\d\.\-_]*[\w\d]$`
	pluginVendorVersionPatt = `^v?(\d+\.){2}(\d+)(-.+)?$`
)

func (s *interceptor) validateGetPluginInfoResponse(
	ctx context.Context,
	rep csi.GetPluginInfoResponse) error {

	log.Debug("validateGetPluginInfoResponse: enter")

	if rep.Name == "" {
		return status.Error(codes.Internal, "empty: Name")
	}
	if l := len(rep.Name); l > pluginNameMax {
		return status.Errorf(codes.Internal,
			"exceeds size limit: Name=%s: max=%d, size=%d",
			rep.Name, pluginNameMax, l)
	}
	nok, err := regexp.MatchString(pluginNamePatt, rep.Name)
	if err != nil {
		return err
	}
	if !nok {
		return status.Errorf(codes.Internal,
			"invalid: Name=%s: patt=%s",
			rep.Name, pluginNamePatt)
	}
	if rep.VendorVersion == "" {
		return status.Error(codes.Internal, "empty: VendorVersion")
	}
	vok, err := regexp.MatchString(pluginVendorVersionPatt, rep.VendorVersion)
	if err != nil {
		return err
	}
	if !vok {
		return status.Errorf(codes.Internal,
			"invalid: VendorVersion=%s: patt=%s",
			rep.VendorVersion, pluginVendorVersionPatt)
	}
	if rep.Manifest != nil && len(rep.Manifest) == 0 {
		return status.Error(codes.Internal,
			"non-nil, empty: Manifest")
	}
	return nil
}

func (s *interceptor) validateNodeGetInfoResponse(
	ctx context.Context,
	rep csi.NodeGetInfoResponse) error {
	if rep.NodeId == "" {
		return status.Error(codes.Internal, "empty: NodeID")
	}

	return nil
}

func (s *interceptor) validateNodeGetCapabilitiesResponse(
	ctx context.Context,
	rep csi.NodeGetCapabilitiesResponse) error {

	if rep.Capabilities != nil && len(rep.Capabilities) == 0 {
		return status.Error(codes.Internal, "non-nil, empty: Capabilities")
	}
	return nil
}

func validateVolumeCapabilityArg(
	volCap *csi.VolumeCapability,
	required bool) error {

	if required && volCap == nil {
		return status.Error(codes.InvalidArgument, "required: VolumeCapability")
	}

	if volCap.AccessMode == nil {
		return status.Error(codes.InvalidArgument, "required: AccessMode")
	}

	atype := volCap.GetAccessType()
	if atype == nil {
		return status.Error(codes.InvalidArgument, "required: AccessType")
	}

	switch tatype := atype.(type) {
	case *csi.VolumeCapability_Block:
		if tatype.Block == nil {
			return status.Error(codes.InvalidArgument,
				"required: AccessType.Block")
		}
	case *csi.VolumeCapability_Mount:
		if tatype.Mount == nil {
			return status.Error(codes.InvalidArgument,
				"required: AccessType.Mount")
		}
	default:
		return status.Errorf(codes.InvalidArgument,
			"invalid: AccessType=%T", atype)
	}

	return nil
}

func validateVolumeCapabilitiesArg(
	volCaps []*csi.VolumeCapability,
	required bool) error {

	if len(volCaps) == 0 {
		if required {
			return status.Error(
				codes.InvalidArgument, "required: VolumeCapabilities")
		}
		return nil
	}

	for i, cap := range volCaps {
		if cap.AccessMode == nil {
			return status.Errorf(
				codes.InvalidArgument,
				"required: VolumeCapabilities[%d].AccessMode", i)
		}
		atype := cap.GetAccessType()
		if atype == nil {
			return status.Errorf(
				codes.InvalidArgument,
				"required: VolumeCapabilities[%d].AccessType", i)
		}
		switch tatype := atype.(type) {
		case *csi.VolumeCapability_Block:
			if tatype.Block == nil {
				return status.Errorf(
					codes.InvalidArgument,
					"required: VolumeCapabilities[%d].AccessType.Block", i)

			}
		case *csi.VolumeCapability_Mount:
			if tatype.Mount == nil {
				return status.Errorf(
					codes.InvalidArgument,
					"required: VolumeCapabilities[%d].AccessType.Mount", i)
			}
		default:
			return status.Errorf(
				codes.InvalidArgument,
				"invalid: VolumeCapabilities[%d].AccessType=%T", i, atype)
		}
	}

	return nil
}

const (
	maxFieldString = 128
	maxFieldMap    = 4096
)

func validateFieldSizes(msg interface{}) error {
	rv := reflect.ValueOf(msg).Elem()
	tv := rv.Type()
	nf := tv.NumField()
	for i := 0; i < nf; i++ {
		f := rv.Field(i)
		switch f.Kind() {
		case reflect.String:
			if l := f.Len(); l > maxFieldString {
				return status.Errorf(
					codes.InvalidArgument,
					"exceeds size limit: %s: max=%d, size=%d",
					tv.Field(i).Name, maxFieldString, l)
			}
		case reflect.Map:
			if f.Len() == 0 {
				continue
			}
			size := 0
			for _, k := range f.MapKeys() {
				if k.Kind() == reflect.String {
					kl := k.Len()
					if kl > maxFieldString {
						return status.Errorf(
							codes.InvalidArgument,
							"exceeds size limit: %s[%s]: max=%d, size=%d",
							tv.Field(i).Name, k.String(), maxFieldString, kl)
					}
					size = size + kl
				}
				if v := f.MapIndex(k); v.Kind() == reflect.String {
					vl := v.Len()
					if vl > maxFieldString {
						return status.Errorf(
							codes.InvalidArgument,
							"exceeds size limit: %s[%s]=: max=%d, size=%d",
							tv.Field(i).Name, k.String(), maxFieldString, vl)
					}
					size = size + vl
				}
			}
			if size > maxFieldMap {
				return status.Errorf(
					codes.InvalidArgument,
					"exceeds size limit: %s: max=%d, size=%d",
					tv.Field(i).Name, maxFieldMap, size)
			}
		}
	}
	return nil
}
