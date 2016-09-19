package raftproxy

import (
	"strings"

	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"github.com/gogo/protobuf/protoc-gen-gogo/generator"
)

type raftProxyGen struct {
	gen *generator.Generator
}

func init() {
	generator.RegisterPlugin(new(raftProxyGen))
}

func (g *raftProxyGen) Init(gen *generator.Generator) {
	g.gen = gen
}

func (g *raftProxyGen) Name() string {
	return "raftproxy"
}

func (g *raftProxyGen) genProxyStruct(s *descriptor.ServiceDescriptorProto) {
	g.gen.P("type " + serviceTypeName(s) + " struct {")
	g.gen.P("\tlocal " + s.GetName() + "Server")
	g.gen.P("\tconnSelector raftselector.ConnProvider")
	g.gen.P("\tctxMods []func(context.Context)(context.Context, error)")
	g.gen.P("}")
}

func (g *raftProxyGen) genProxyConstructor(s *descriptor.ServiceDescriptorProto) {
	g.gen.P("func NewRaftProxy" + s.GetName() + "Server(local " + s.GetName() + "Server, connSelector raftselector.ConnProvider, ctxMod func(context.Context)(context.Context, error)) " + s.GetName() + "Server {")
	g.gen.P(`redirectChecker := func(ctx context.Context)(context.Context, error) {
		s, ok := transport.StreamFromContext(ctx)
		if !ok {
			return ctx, grpc.Errorf(codes.InvalidArgument, "remote addr is not found in context")
		}
		addr := s.ServerTransport().RemoteAddr().String()
		md, ok := metadata.FromContext(ctx)
		if ok && len(md["redirect"]) != 0 {
			return ctx, grpc.Errorf(codes.ResourceExhausted, "more than one redirect to leader from: %s", md["redirect"])
		}
		if !ok {
			md = metadata.New(map[string]string{})
		}
		md["redirect"] = append(md["redirect"], addr)
		return metadata.NewContext(ctx, md), nil
	}
	mods := []func(context.Context)(context.Context, error){redirectChecker}
	mods = append(mods, ctxMod)
	`)
	g.gen.P("return &" + serviceTypeName(s) + `{
		local: local,
		connSelector: connSelector,
		ctxMods: mods,
	}`)
	g.gen.P("}")
}

func (g *raftProxyGen) genRunCtxMods(s *descriptor.ServiceDescriptorProto) {
	g.gen.P("func (p *" + serviceTypeName(s) + `) runCtxMods(ctx context.Context) (context.Context, error) {
	var err error
	for _, mod := range p.ctxMods {
		ctx, err = mod(ctx)
		if err != nil {
			return ctx, err
		}
	}
	return ctx, nil
}`)
}

func getInputTypeName(m *descriptor.MethodDescriptorProto) string {
	parts := strings.Split(m.GetInputType(), ".")
	return parts[len(parts)-1]
}

func getOutputTypeName(m *descriptor.MethodDescriptorProto) string {
	parts := strings.Split(m.GetOutputType(), ".")
	return parts[len(parts)-1]
}

func serviceTypeName(s *descriptor.ServiceDescriptorProto) string {
	return "raftProxy" + s.GetName() + "Server"
}

func sigPrefix(s *descriptor.ServiceDescriptorProto, m *descriptor.MethodDescriptorProto) string {
	return "func (p *" + serviceTypeName(s) + ") " + m.GetName() + "("
}

func (g *raftProxyGen) genClientStreamingMethod(s *descriptor.ServiceDescriptorProto, m *descriptor.MethodDescriptorProto) {
	g.gen.P(sigPrefix(s, m) + "stream " + s.GetName() + "_" + m.GetName() + "Server) error {")
	g.gen.P(`
	ctx := stream.Context()
	conn, err := p.connSelector.LeaderConn(ctx)
	if err != nil {
		if err == raftselector.ErrIsLeader {
			return p.local.` + m.GetName() + `(stream)
		}
		return err
	}
	ctx, err = p.runCtxMods(ctx)
	if err != nil {
		return err
	}`)
	g.gen.P("clientStream, err := New" + s.GetName() + "Client(conn)." + m.GetName() + "(ctx)")
	g.gen.P(`
	if err != nil {
			return err
	}`)
	g.gen.P(`
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if err := clientStream.Send(msg); err != nil {
			return err
		}
	}

	reply, err := clientStream.CloseAndRecv()
	if err != nil {
		return err
	}

	return stream.SendAndClose(reply)`)
	g.gen.P("}")
}

func (g *raftProxyGen) genServerStreamingMethod(s *descriptor.ServiceDescriptorProto, m *descriptor.MethodDescriptorProto) {
	g.gen.P(sigPrefix(s, m) + "r *" + getInputTypeName(m) + ", stream " + s.GetName() + "_" + m.GetName() + "Server) error {")
	g.gen.P(`
	ctx := stream.Context()
	conn, err := p.connSelector.LeaderConn(ctx)
	if err != nil {
		if err == raftselector.ErrIsLeader {
			return p.local.` + m.GetName() + `(r, stream)
		}
		return err
	}
	ctx, err = p.runCtxMods(ctx)
	if err != nil {
		return err
	}`)
	g.gen.P("clientStream, err := New" + s.GetName() + "Client(conn)." + m.GetName() + "(ctx, r)")
	g.gen.P(`
	if err != nil {
			return err
	}`)
	g.gen.P(`
	for {
		msg, err := clientStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if err := stream.Send(msg); err != nil {
			return err
		}
	}
	return nil`)
	g.gen.P("}")
}

func (g *raftProxyGen) genClientServerStreamingMethod(s *descriptor.ServiceDescriptorProto, m *descriptor.MethodDescriptorProto) {
	g.gen.P(sigPrefix(s, m) + "stream " + s.GetName() + "_" + m.GetName() + "Server) error {")
	g.gen.P(`
	ctx := stream.Context()
	conn, err := p.connSelector.LeaderConn(ctx)
	if err != nil {
		if err == raftselector.ErrIsLeader {
			return p.local.` + m.GetName() + `(stream)
		}
		return err
	}
	ctx, err = p.runCtxMods(ctx)
	if err != nil {
		return err
	}`)
	g.gen.P("clientStream, err := New" + s.GetName() + "Client(conn)." + m.GetName() + "(ctx)")
	g.gen.P(`
	if err != nil {
			return err
	}`)
	g.gen.P(`errc := make(chan error, 1)
	go func() {
		msg, err := stream.Recv()
		if err == io.EOF {
			close(errc)
			return
		}
		if err != nil {
			errc <- err
			return
		}
		if err := clientStream.Send(msg); err != nil {
			errc <- err
			return
		}
	}()`)
	g.gen.P(`
	for {
		msg, err := clientStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if err := stream.Send(msg); err != nil {
			return err
		}
	}
	clientStream.CloseSend()
	return <-errc`)
	g.gen.P("}")
}

func (g *raftProxyGen) genSimpleMethod(s *descriptor.ServiceDescriptorProto, m *descriptor.MethodDescriptorProto) {
	g.gen.P(sigPrefix(s, m) + "ctx context.Context, r *" + getInputTypeName(m) + ") (*" + getOutputTypeName(m) + ", error) {")
	g.gen.P(`
	conn, err := p.connSelector.LeaderConn(ctx)
	if err != nil {
		if err == raftselector.ErrIsLeader {
			return p.local.` + m.GetName() + `(ctx, r)
		}
		return nil, err
	}
	modCtx, err := p.runCtxMods(ctx)
	if err != nil {
		return nil, err
	}`)
	g.gen.P(`
	resp, err := New` + s.GetName() + `Client(conn).` + m.GetName() + `(modCtx, r)
	if err != nil {
		if !strings.Contains(err.Error(), "is closing") && !strings.Contains(err.Error(), "the connection is unavailable") && !strings.Contains(err.Error(), "connection error") {
			return resp, err
		}
		conn, err := p.pollNewLeaderConn(ctx)
		if err != nil {
			if err == raftselector.ErrIsLeader {
				return p.local.` + m.GetName() + `(ctx, r)
			}
			return nil, err
		}
		return New` + s.GetName() + `Client(conn).` + m.GetName() + `(modCtx, r)
	}`)
	g.gen.P("return resp, err")
	g.gen.P("}")
}

func (g *raftProxyGen) genProxyMethod(s *descriptor.ServiceDescriptorProto, m *descriptor.MethodDescriptorProto) {
	g.gen.P()
	switch {
	case m.GetServerStreaming() && m.GetClientStreaming():
		g.genClientServerStreamingMethod(s, m)
	case m.GetServerStreaming():
		g.genServerStreamingMethod(s, m)
	case m.GetClientStreaming():
		g.genClientStreamingMethod(s, m)
	default:
		g.genSimpleMethod(s, m)
	}
	g.gen.P()
}

func (g *raftProxyGen) genPollNewLeaderConn(s *descriptor.ServiceDescriptorProto) {
	g.gen.P(`func (p *` + serviceTypeName(s) + `) pollNewLeaderConn(ctx context.Context) (*grpc.ClientConn, error) {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				conn, err := p.connSelector.LeaderConn(ctx)
				if err != nil {
					return nil, err
				}

				client := NewHealthClient(conn)

				resp, err := client.Check(ctx, &HealthCheckRequest{Service: "Raft"})
				if err != nil || resp.Status != HealthCheckResponse_SERVING {
					continue
				}
				return conn, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}`)
}

func (g *raftProxyGen) Generate(file *generator.FileDescriptor) {
	g.gen.P()
	for _, s := range file.Service {
		g.genProxyStruct(s)
		g.genProxyConstructor(s)
		g.genRunCtxMods(s)
		g.genPollNewLeaderConn(s)
		for _, m := range s.Method {
			g.genProxyMethod(s, m)
		}
	}
	g.gen.P()
}

func (g *raftProxyGen) GenerateImports(file *generator.FileDescriptor) {
	if len(file.Service) == 0 {
		return
	}
	g.gen.P("import raftselector \"github.com/docker/swarmkit/manager/raftselector\"")
	g.gen.P("import codes \"google.golang.org/grpc/codes\"")
	g.gen.P("import metadata \"google.golang.org/grpc/metadata\"")
	g.gen.P("import transport \"google.golang.org/grpc/transport\"")
	g.gen.P("import time \"time\"")
}
