package ca

import (
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

const (
	certCNKey = "forwarded_cert_cn"
)

// forwardCNFromContext obtains ForwardCert from grpc.MD object in context.
func forwardCNFromContext(ctx context.Context) (string, error) {
	md, _ := metadata.FromContext(ctx)
	if len(md[certCNKey]) != 0 {
		return md[certCNKey][0], nil
	}
	return "", grpc.Errorf(codes.PermissionDenied, "Permission denied: forwarded request without agent info")
}

// WithMetadataForwardCN reads certificate from context and returns context where
// ForwardCert is set based on original certificate.
func WithMetadataForwardCN(ctx context.Context) (context.Context, error) {
	// only agents can reach this codepath
	cn, err := AuthorizeRole(ctx, []string{AgentRole})
	if err != nil {
		return nil, err
	}
	md, ok := metadata.FromContext(ctx)
	if !ok {
		md = metadata.MD{}
	}
	md[certCNKey] = []string{cn}
	return metadata.NewContext(ctx, md), nil
}
