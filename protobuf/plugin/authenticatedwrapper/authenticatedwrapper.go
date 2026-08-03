package authenticatedwrapper

import (
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/moby/swarmkit/v2/protobuf/plugin"
)

// Generate emits, for every service in f, an authenticatedWrapper<Svc>Server
// that consults an authorize hook before delegating to the wrapped server.
//
// The roles handed to the hook come from the tls_authorization method option;
// methods marked insecure delegate without a check, and methods carrying no
// option at all get a body that panics, so that forgetting to annotate a new
// RPC fails loudly instead of silently serving it unauthenticated.
func Generate(g *protogen.GeneratedFile, f *protogen.File) {
	for _, s := range f.Services {
		genService(g, s)
	}
}

func genService(g *protogen.GeneratedFile, s *protogen.Service) {
	name := serviceTypeName(s)
	ctx := g.QualifiedGoIdent(protogen.GoIdent{GoName: "Context", GoImportPath: "context"})

	g.P("type ", name, " struct {")
	g.P("local ", s.GoName, "Server")
	g.P("authorize func(", ctx, ", []string) error")
	g.P("}")
	g.P()

	g.P("func NewAuthenticatedWrapper", s.GoName, "Server(local ", s.GoName, "Server, authorize func(", ctx, ", []string) error) ", s.GoName, "Server {")
	g.P("return &", name, "{")
	g.P("local: local,")
	g.P("authorize: authorize,")
	g.P("}")
	g.P("}")
	g.P()

	// protoc-gen-go-grpc guards <Svc>Server with an unexported method. Provide
	// it explicitly rather than embedding Unimplemented<Svc>Server: embedding
	// would make any method this generator fails to emit compile fine and then
	// bypass authorization at runtime.
	g.P("func (p *", name, ") mustEmbedUnimplemented", s.GoName, "Server() {}")
	g.P()

	for _, m := range s.Methods {
		genMethod(g, s, m)
	}
}

func genMethod(g *protogen.GeneratedFile, s *protogen.Service, m *protogen.Method) {
	name := serviceTypeName(s)

	// The delegation call, the context the hook is given and the shape of an
	// early error return all depend on the method's streaming kind.
	var args, authCtx, failure string
	switch {
	case m.Desc.IsStreamingClient():
		// Client-streaming and bidirectional methods only take the stream.
		g.P("func (p *", name, ") ", m.GoName, "(stream ", streamTypeName(s, m), ") error {")
		args, authCtx, failure = "stream", "stream.Context()", "return err"
	case m.Desc.IsStreamingServer():
		g.P("func (p *", name, ") ", m.GoName, "(r *", g.QualifiedGoIdent(m.Input.GoIdent), ", stream ", streamTypeName(s, m), ") error {")
		args, authCtx, failure = "r, stream", "stream.Context()", "return err"
	default:
		ctx := g.QualifiedGoIdent(protogen.GoIdent{GoName: "Context", GoImportPath: "context"})
		g.P("func (p *", name, ") ", m.GoName, "(ctx ", ctx, ", r *", g.QualifiedGoIdent(m.Input.GoIdent), ") (*", g.QualifiedGoIdent(m.Output.GoIdent), ", error) {")
		args, authCtx, failure = "ctx, r", "ctx", "return nil, err"
	}

	auth := tlsAuthorization(m)
	switch {
	case auth == nil:
		g.P(`panic("no authorization information in protobuf")`)
	case auth.GetInsecure():
		if len(auth.GetRoles()) != 0 {
			panic("Roles and Insecure cannot both be specified")
		}
		g.P("return p.local.", m.GoName, "(", args, ")")
	default:
		g.P("if err := p.authorize(", authCtx, ", ", genRoles(auth), "); err != nil {")
		g.P(failure)
		g.P("}")
		g.P("return p.local.", m.GoName, "(", args, ")")
	}
	g.P("}")
	g.P()
}

// tlsAuthorization returns the tls_authorization option of m, or nil if the
// method carries none.
//
// Presence has to be tested separately: unlike gogo's, [proto.GetExtension]
// never reports an error and hands back an empty message for an unset
// extension, which is indistinguishable from an explicitly empty one.
func tlsAuthorization(m *protogen.Method) *plugin.TLSAuthorization {
	opts, _ := m.Desc.Options().(*descriptorpb.MethodOptions)
	if opts == nil || !proto.HasExtension(opts, plugin.E_TlsAuthorization) {
		return nil
	}
	auth, _ := proto.GetExtension(opts, plugin.E_TlsAuthorization).(*plugin.TLSAuthorization)
	return auth
}

func serviceTypeName(s *protogen.Service) string {
	return "authenticatedWrapper" + s.GoName + "Server"
}

// streamTypeName is the server-side stream type protoc-gen-go-grpc declares for
// m. It is a generic alias nowadays, but keeps the historical spelling.
func streamTypeName(s *protogen.Service, m *protogen.Method) string {
	return s.GoName + "_" + m.GoName + "Server"
}

// genRoles renders the option's roles as a Go string slice literal.
func genRoles(auth *plugin.TLSAuthorization) string {
	var roles strings.Builder
	roles.WriteString("[]string{")
	for i, role := range auth.GetRoles() {
		if i > 0 {
			roles.WriteString(",")
		}
		roles.WriteString(`"` + role + `"`)
	}
	roles.WriteString("}")

	return roles.String()
}
