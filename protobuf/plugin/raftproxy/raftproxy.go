package raftproxy

import (
	"google.golang.org/protobuf/compiler/protogen"
)

// Packages the generated proxies reference. protogen registers the import and
// picks a non-conflicting local name, so the generator never spells one out.
const (
	contextPkg      = protogen.GoImportPath("context")
	ioPkg           = protogen.GoImportPath("io")
	stringsPkg      = protogen.GoImportPath("strings")
	timePkg         = protogen.GoImportPath("time")
	grpcPkg         = protogen.GoImportPath("google.golang.org/grpc")
	codesPkg        = protogen.GoImportPath("google.golang.org/grpc/codes")
	metadataPkg     = protogen.GoImportPath("google.golang.org/grpc/metadata")
	peerPkg         = protogen.GoImportPath("google.golang.org/grpc/peer")
	statusPkg       = protogen.GoImportPath("google.golang.org/grpc/status")
	raftselectorPkg = protogen.GoImportPath("github.com/moby/swarmkit/v2/manager/raftselector")
)

// Generate emits, for every service in f, a raftProxy<Svc>Server: a
// <Svc>Server implementation that forwards every call to the current raft
// leader and only serves it locally when this node is the leader itself.
func Generate(g *protogen.GeneratedFile, f *protogen.File) {
	for _, s := range f.Services {
		genProxyStruct(g, s)
		genProxyConstructor(g, s)
		genRunCtxMods(g, s)
		genPollNewLeaderConn(g, s)
		for _, m := range s.Methods {
			genProxyMethod(g, s, m)
		}
	}
}

// ident qualifies name from path, adding the import as a side effect.
func ident(g *protogen.GeneratedFile, path protogen.GoImportPath, name string) string {
	return g.QualifiedGoIdent(path.Ident(name))
}

// proxyName is the unexported name of the proxy type for s.
func proxyName(s *protogen.Service) string {
	return "raftProxy" + s.GoName + "Server"
}

// ctxModType is the type of the context modifiers callers hand to the
// constructor, spelled with a qualified context.Context.
func ctxModType(g *protogen.GeneratedFile) string {
	ctx := ident(g, contextPkg, "Context")
	return "func(" + ctx + ") (" + ctx + ", error)"
}

// streamTypeName is the server-side stream type protoc-gen-go-grpc emits for m.
// Since v1.6 those are aliases for the generic grpc.*StreamingServer
// interfaces, but the names are unchanged and they are still embeddable, which
// is all the stream wrappers need.
func streamTypeName(s *protogen.Service, m *protogen.Method) string {
	return s.GoName + "_" + m.GoName + "Server"
}

func genProxyStruct(g *protogen.GeneratedFile, s *protogen.Service) {
	g.P("type ", proxyName(s), " struct {")
	g.P("local ", s.GoName, "Server")
	g.P("connSelector ", ident(g, raftselectorPkg, "ConnProvider"))
	g.P("localCtxMods, remoteCtxMods []", ctxModType(g))
	g.P("}")
	g.P()

	// protoc-gen-go-grpc guards <Svc>Server with an unexported method to force
	// implementations to embed Unimplemented<Svc>Server. The proxy implements
	// every method itself, so satisfy the guard explicitly rather than
	// embedding: with the embedded struct, a method this generator failed to
	// emit would silently resolve to an "unimplemented" stub instead of
	// failing to compile.
	g.P("func (p *", proxyName(s), ") mustEmbedUnimplemented", s.GoName, "Server() {}")
	g.P()
}

func genProxyConstructor(g *protogen.GeneratedFile, s *protogen.Service) {
	ctx := ident(g, contextPkg, "Context")
	mod := ctxModType(g)

	g.P("func NewRaftProxy", s.GoName, "Server(local ", s.GoName, "Server, connSelector ",
		ident(g, raftselectorPkg, "ConnProvider"), ", localCtxMod, remoteCtxMod ", mod, ") ", s.GoName, "Server {")
	g.P(`redirectChecker := func(ctx ` + ctx + `) (` + ctx + `, error) {
	p, ok := ` + ident(g, peerPkg, "FromContext") + `(ctx)
	if !ok {
		return ctx, ` + ident(g, statusPkg, "Errorf") + `(` + ident(g, codesPkg, "InvalidArgument") + `, "remote addr is not found in context")
	}
	addr := p.Addr.String()
	md, ok := ` + ident(g, metadataPkg, "FromIncomingContext") + `(ctx)
	if ok && len(md["redirect"]) != 0 {
		return ctx, ` + ident(g, statusPkg, "Errorf") + `(` + ident(g, codesPkg, "ResourceExhausted") + `, "more than one redirect to leader from: %s", md["redirect"])
	}
	if !ok {
		md = ` + ident(g, metadataPkg, "New") + `(map[string]string{})
	}
	md["redirect"] = append(md["redirect"], addr)
	return ` + ident(g, metadataPkg, "NewOutgoingContext") + `(ctx, md), nil
}
remoteMods := []` + mod + `{redirectChecker}
remoteMods = append(remoteMods, remoteCtxMod)

var localMods []` + mod + `
if localCtxMod != nil {
	localMods = []` + mod + `{localCtxMod}
}
`)
	g.P("return &", proxyName(s), `{
	local: local,
	connSelector: connSelector,
	localCtxMods: localMods,
	remoteCtxMods: remoteMods,
}`)
	g.P("}")
	g.P()
}

func genRunCtxMods(g *protogen.GeneratedFile, s *protogen.Service) {
	ctx := ident(g, contextPkg, "Context")
	g.P("func (p *", proxyName(s), ") runCtxMods(ctx ", ctx, ", ctxMods []", ctxModType(g), ") (", ctx, `, error) {
	var err error
	for _, mod := range ctxMods {
		ctx, err = mod(ctx)
		if err != nil {
			return ctx, err
		}
	}
	return ctx, nil
}`)
	g.P()
}

// genPollNewLeaderConn emits the helper the unary methods use to wait for a new
// leader once the connection to the previous one broke. The candidate is probed
// through the Health service, which every package that carries a raft-proxied
// service also defines.
func genPollNewLeaderConn(g *protogen.GeneratedFile, s *protogen.Service) {
	g.P("func (p *", proxyName(s), ") pollNewLeaderConn(ctx ", ident(g, contextPkg, "Context"), ") (*",
		ident(g, grpcPkg, "ClientConn"), `, error) {
	ticker := `+ident(g, timePkg, "NewTicker")+`(500 * `+ident(g, timePkg, "Millisecond")+`)
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
	g.P()
}

func genProxyMethod(g *protogen.GeneratedFile, s *protogen.Service, m *protogen.Method) {
	switch {
	case m.Desc.IsStreamingClient() && m.Desc.IsStreamingServer():
		genClientServerStreamingMethod(g, s, m)
	case m.Desc.IsStreamingServer():
		genServerStreamingMethod(g, s, m)
	case m.Desc.IsStreamingClient():
		genClientStreamingMethod(g, s, m)
	default:
		genSimpleMethod(g, s, m)
	}
	g.P()
}

// genStreamWrapper emits a wrapper around the server stream that hands out the
// context the local ctx mods produced instead of the one gRPC created.
func genStreamWrapper(g *protogen.GeneratedFile, streamType string) {
	ctx := ident(g, contextPkg, "Context")
	g.P("type ", streamType, `Wrapper struct {
	`+streamType+`
	ctx `+ctx+`
}`)
	g.P()
	g.P("func (s ", streamType, "Wrapper) Context() ", ctx, ` {
	return s.ctx
}`)
	g.P()
}

// genLocalStreamFallback emits the preamble shared by every streaming method:
// grab the leader connection, and if this node is the leader serve the call
// locally through the context-overriding stream wrapper. localArgs holds the
// arguments the local call takes before the stream, trailing comma included.
func genLocalStreamFallback(g *protogen.GeneratedFile, m *protogen.Method, streamType, localArgs string) {
	g.P(`ctx := stream.Context()
conn, err := p.connSelector.LeaderConn(ctx)
if err != nil {
	if err == ` + ident(g, raftselectorPkg, "ErrIsLeader") + ` {
		ctx, err = p.runCtxMods(ctx, p.localCtxMods)
		if err != nil {
			return err
		}
		streamWrapper := ` + streamType + `Wrapper{
			` + streamType + `: stream,
			ctx: ctx,
		}
		return p.local.` + m.GoName + `(` + localArgs + `streamWrapper)
	}
	return err
}
ctx, err = p.runCtxMods(ctx, p.remoteCtxMods)
if err != nil {
	return err
}`)
}

func genClientStreamingMethod(g *protogen.GeneratedFile, s *protogen.Service, m *protogen.Method) {
	streamType := streamTypeName(s, m)
	genStreamWrapper(g, streamType)

	g.P("func (p *", proxyName(s), ") ", m.GoName, "(stream ", streamType, ") error {")
	genLocalStreamFallback(g, m, streamType, "")
	g.P("clientStream, err := New", s.GoName, "Client(conn).", m.GoName, `(ctx)
if err != nil {
	return err
}

for {
	msg, err := stream.Recv()
	if err == `+ident(g, ioPkg, "EOF")+` {
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
	g.P("}")
}

func genServerStreamingMethod(g *protogen.GeneratedFile, s *protogen.Service, m *protogen.Method) {
	streamType := streamTypeName(s, m)
	genStreamWrapper(g, streamType)

	g.P("func (p *", proxyName(s), ") ", m.GoName, "(r *", g.QualifiedGoIdent(m.Input.GoIdent), ", stream ", streamType, ") error {")
	genLocalStreamFallback(g, m, streamType, "r, ")
	g.P("clientStream, err := New", s.GoName, "Client(conn).", m.GoName, `(ctx, r)
if err != nil {
	return err
}

for {
	msg, err := clientStream.Recv()
	if err == `+ident(g, ioPkg, "EOF")+` {
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
	g.P("}")
}

func genClientServerStreamingMethod(g *protogen.GeneratedFile, s *protogen.Service, m *protogen.Method) {
	streamType := streamTypeName(s, m)
	genStreamWrapper(g, streamType)

	eof := ident(g, ioPkg, "EOF")

	g.P("func (p *", proxyName(s), ") ", m.GoName, "(stream ", streamType, ") error {")
	genLocalStreamFallback(g, m, streamType, "")
	g.P("clientStream, err := New", s.GoName, "Client(conn).", m.GoName, `(ctx)
if err != nil {
	return err
}
errc := make(chan error, 1)
go func() {
	msg, err := stream.Recv()
	if err == `+eof+` {
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
}()

for {
	msg, err := clientStream.Recv()
	if err == `+eof+` {
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
	g.P("}")
}

func genSimpleMethod(g *protogen.GeneratedFile, s *protogen.Service, m *protogen.Method) {
	contains := ident(g, stringsPkg, "Contains")
	errIsLeader := ident(g, raftselectorPkg, "ErrIsLeader")

	g.P("func (p *", proxyName(s), ") ", m.GoName, "(ctx ", ident(g, contextPkg, "Context"),
		", r *", g.QualifiedGoIdent(m.Input.GoIdent), ") (*", g.QualifiedGoIdent(m.Output.GoIdent), ", error) {")
	g.P(`conn, err := p.connSelector.LeaderConn(ctx)
if err != nil {
	if err == ` + errIsLeader + ` {
		ctx, err = p.runCtxMods(ctx, p.localCtxMods)
		if err != nil {
			return nil, err
		}
		return p.local.` + m.GoName + `(ctx, r)
	}
	return nil, err
}
modCtx, err := p.runCtxMods(ctx, p.remoteCtxMods)
if err != nil {
	return nil, err
}
`)
	// A transport-level failure means the leader we picked is gone: wait for a
	// new one and retry once. Any other error is the leader's own answer and is
	// handed back to the caller untouched.
	g.P("resp, err := New", s.GoName, "Client(conn).", m.GoName, `(modCtx, r)
if err != nil {
	if !`+contains+`(err.Error(), "is closing") && !`+contains+`(err.Error(), "the connection is unavailable") && !`+contains+`(err.Error(), "connection error") {
		return resp, err
	}
	conn, err := p.pollNewLeaderConn(ctx)
	if err != nil {
		if err == `+errIsLeader+` {
			return p.local.`+m.GoName+`(ctx, r)
		}
		return nil, err
	}
	return New`+s.GoName+`Client(conn).`+m.GoName+`(modCtx, r)
}`)
	g.P("return resp, err")
	g.P("}")
}
