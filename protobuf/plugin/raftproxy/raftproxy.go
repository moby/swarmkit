package raftproxy

import (
	"fmt"
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
	g.gen.P("\tconn *grpc.ClientConn")
	g.gen.P("\tcluster raftpicker.RaftCluster")
	g.gen.P("\tconnOnce sync.Once")
	g.gen.P("}")
}

func (g *raftProxyGen) genProxyConstructor(s *descriptor.ServiceDescriptorProto) {
	g.gen.P("func NewRaftProxy" + s.GetName() + "Server(local " + s.GetName() + "Server, cluster raftpicker.RaftCluster) (" + s.GetName() + "Server, error) {")
	g.gen.P("return &" + serviceTypeName(s) + `{
		local: local,
		cluster: cluster,
	}, nil`)
	g.gen.P("}")
}

func (g *raftProxyGen) genProxyInitConn(s *descriptor.ServiceDescriptorProto) {
	g.gen.P(`func (p *` + serviceTypeName(s) + `) initConn() error {
		var err error
		p.connOnce.Do(func() {
			cLeader, leadErr := p.cluster.LeaderAddr()
			if err != nil {
				err = leadErr
				return
			}
			p.conn, err = grpc.Dial(cLeader, grpc.WithInsecure(), grpc.WithPicker(raftpicker.New(p.cluster)))
		})
		if err != nil {
			return grpc.Errorf(codes.Internal, err.Error())
		}
		return nil
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

func (g *raftProxyGen) genAddrObtain(ctxStr string) {
	g.gen.P(fmt.Sprintf(`var addr string
	s, ok := transport.StreamFromContext(%s)
	if ok {
		addr = s.ServerTransport().RemoteAddr().String()
	}`, ctxStr))
}

func (g *raftProxyGen) genStreamRedirectCheck() {
	g.genAddrObtain("stream.Context()")
	g.gen.P(`md, ok := metadata.FromContext(stream.Context())
	if ok && len(md["redirect"]) != 0 {
		return grpc.Errorf(codes.ResourceExhausted, "more than one redirect to leader from: %s", md["redirect"])
	}
	if !ok {
		md = metadata.New(map[string]string{})
	}
	md["redirect"] = append(md["redirect"], addr)
	ctx := metadata.NewContext(stream.Context(), md)
	`)
}

func (g *raftProxyGen) genClientStreamingMethod(s *descriptor.ServiceDescriptorProto, m *descriptor.MethodDescriptorProto) {
	g.gen.P(sigPrefix(s, m) + "stream " + s.GetName() + "_" + m.GetName() + "Server) error {")
	g.gen.P(`
	if p.cluster.IsLeader() {
			return p.local.` + m.GetName() + `(stream)
	}
	if err := p.initConn(); err != nil {
		return err
	}`)
	g.genStreamRedirectCheck()
	g.gen.P("clientStream, err := New" + s.GetName() + "Client(p.conn)." + m.GetName() + "(ctx)")
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
	if p.cluster.IsLeader() {
			return p.local.` + m.GetName() + `(r, stream)
	}
	if err := p.initConn(); err != nil {
		return err
	}`)
	g.genStreamRedirectCheck()
	g.gen.P("clientStream, err := New" + s.GetName() + "Client(p.conn)." + m.GetName() + "(ctx, r)")
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
	if p.cluster.IsLeader() {
			return p.local.` + m.GetName() + `(stream)
	}
	if err := p.initConn(); err != nil {
		return err
	}`)
	g.genStreamRedirectCheck()
	g.gen.P("clientStream, err := New" + s.GetName() + "Client(p.conn)." + m.GetName() + "(ctx)")
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
	if p.cluster.IsLeader() {
			return p.local.` + m.GetName() + `(ctx, r)
	}
	if err := p.initConn(); err != nil {
		return nil, err
	}
	`)
	g.genAddrObtain("ctx")
	g.gen.P(`md, ok := metadata.FromContext(ctx)
	if ok && len(md["redirect"]) != 0 {
		return nil, grpc.Errorf(codes.ResourceExhausted, "more than one redirect to leader from: %s", md["redirect"])
	}
	if !ok {
		md = metadata.New(map[string]string{})
	}
	md["redirect"] = append(md["redirect"], addr)
	ctx = metadata.NewContext(ctx, md)
	`)

	g.gen.P("return New" + s.GetName() + "Client(p.conn)." + m.GetName() + "(ctx, r)")
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

func (g *raftProxyGen) Generate(file *generator.FileDescriptor) {
	g.gen.P()
	for _, s := range file.Service {
		g.genProxyStruct(s)
		g.genProxyConstructor(s)
		g.genProxyInitConn(s)
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
	g.gen.P("import raftpicker \"github.com/docker/swarm-v2/manager/raftpicker\"")
	g.gen.P("import codes \"google.golang.org/grpc/codes\"")
	g.gen.P("import metadata \"google.golang.org/grpc/metadata\"")
	g.gen.P("import transport \"google.golang.org/grpc/transport\"")
	g.gen.P("import sync \"sync\"")
}
