package storeobject

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/moby/swarmkit/v2/protobuf/plugin"
)

var (
	eventsPkg  = protogen.GoImportPath("github.com/docker/go-events")
	stringsPkg = protogen.GoImportPath("strings")
)

// The identifiers below are declared in watch.proto and raft.proto, which
// objects.proto does not import. They are therefore not reachable through this
// file's descriptor and have to be spelled out. These are the stock
// protoc-gen-go names for those enum values.
const (
	watchActionCreate = "WatchActionKind_WATCH_ACTION_CREATE"
	watchActionUpdate = "WatchActionKind_WATCH_ACTION_UPDATE"
	watchActionRemove = "WatchActionKind_WATCH_ACTION_REMOVE"

	storeActionCreate = "StoreActionKind_STORE_ACTION_CREATE"
	storeActionUpdate = "StoreActionKind_STORE_ACTION_UPDATE"
	storeActionRemove = "StoreActionKind_STORE_ACTION_REMOVE"
)

// hostnameNamedObject is the one object whose watch "name" is not its
// annotations name: a node is matched by the hostname its agent reports.
//
// FIXME(aaronl): Look at fields inside the descriptor instead of
// special-casing based on name.
const hostnameNamedObject = "Node"

// storeObject is a message that opted into the store_object option, resolved
// down to the descriptor fields the generated code needs to touch.
type storeObject struct {
	msg *protogen.Message
	sel *plugin.WatchSelectors

	// name is the message's Go type name, e.g. "Node".
	name string

	// annPath is the field chain from the object to its Annotations message,
	// either {annotations} or {spec, annotations}. Tasks, resources and
	// extensions carry their annotations directly; every other object keeps
	// them in its spec. Deriving the path from the descriptor replaces the
	// hand-maintained list of "types with no spec" the gogo generator needed.
	annPath []*protogen.Field

	// hostnamePath is the field chain to the object's hostname, set only for
	// hostnameNamedObject.
	hostnamePath []*protogen.Field
}

// Generate emits the store object, event and watch plumbing for every message
// in f that carries the (docker.protobuf.plugin.store_object) option.
func Generate(g *protogen.GeneratedFile, f *protogen.File) {
	var objs []*storeObject
	for _, m := range f.Messages {
		objs = collect(objs, m)
	}
	if len(objs) == 0 {
		return
	}

	for _, o := range objs {
		o.genEventTypes(g)
		o.genStoreObjectMethods(g)
		o.genCheckFuncs(g)
		o.genConvertWatch(g)
		o.genIndexers(g)
	}

	genNewStoreAction(g, objs)
	genEventFromStoreAction(g, objs)

	// for watch API
	genWatchMessageEvent(g, objs)
	genConvertWatchArgs(g, objs)
}

// collect appends m and its nested messages to objs, in declaration order,
// keeping only the ones that enabled the store_object option.
func collect(objs []*storeObject, m *protogen.Message) []*storeObject {
	if o := newStoreObject(m); o != nil {
		objs = append(objs, o)
	}
	for _, nested := range m.Messages {
		objs = collect(objs, nested)
	}
	return objs
}

// newStoreObject returns the resolved store object for m, or nil when m did
// not enable the store_object option.
func newStoreObject(m *protogen.Message) *storeObject {
	// Map entries are synthesized messages with no Go type of their own.
	if m.Desc.IsMapEntry() {
		return nil
	}
	opts, _ := m.Desc.Options().(*descriptorpb.MessageOptions)
	if opts == nil || !proto.HasExtension(opts, plugin.E_StoreObject) {
		return nil
	}
	storeObj, ok := proto.GetExtension(opts, plugin.E_StoreObject).(*plugin.StoreObject)
	if !ok || storeObj == nil {
		return nil
	}

	o := &storeObject{
		msg:  m,
		sel:  storeObj.GetWatchSelectors(),
		name: m.GoIdent.GoName,
	}

	// Annotations either sit on the object itself or one level down, in its
	// spec.
	if ann := field(m, "annotations"); ann != nil {
		o.annPath = []*protogen.Field{ann}
	} else if spec := field(m, "spec"); spec != nil && spec.Message != nil {
		if ann := field(spec.Message, "annotations"); ann != nil {
			o.annPath = []*protogen.Field{spec, ann}
		}
	}

	if o.name == hostnameNamedObject {
		desc := mustField(m, "description")
		o.hostnamePath = []*protogen.Field{desc, mustField(desc.Message, "hostname")}
	}

	return o
}

// field returns m's field with the given protobuf name, or nil when m does not
// declare it.
func field(m *protogen.Message, name string) *protogen.Field {
	for _, f := range m.Fields {
		if string(f.Desc.Name()) == name {
			return f
		}
	}
	return nil
}

// mustField is field for the cases where the option asking for the field is
// meaningless without it; a missing field is a bug in the .proto.
func mustField(m *protogen.Message, name string) *protogen.Field {
	f := field(m, name)
	if f == nil {
		panic(fmt.Sprintf("storeobject: message %s has no %q field", m.Desc.FullName(), name))
	}
	return f
}

// getterChain builds a nil-safe accessor expression such as
// "v1.GetSpec().GetAnnotations().GetName()".
func getterChain(recv string, fields ...*protogen.Field) string {
	var b strings.Builder
	b.WriteString(recv)
	for _, f := range fields {
		b.WriteString(".Get")
		b.WriteString(f.GoName)
		b.WriteString("()")
	}
	return b.String()
}

// fieldChain builds an assignable path such as "m.GetSpec().GetAnnotations().GetName()". Only
// safe on values whose intermediate messages are known to be non-nil.
func fieldChain(recv string, fields ...*protogen.Field) string {
	parts := make([]string, 0, len(fields)+1)
	parts = append(parts, recv)
	for _, f := range fields {
		parts = append(parts, f.GoName)
	}
	return strings.Join(parts, ".")
}

// annMsg returns the object's Annotations message descriptor.
func (o *storeObject) annMsg() *protogen.Message {
	return o.annPath[len(o.annPath)-1].Message
}

// annPathTo returns the field chain from the object down to the named field of
// its Annotations.
func (o *storeObject) annPathTo(name string) []*protogen.Field {
	path := make([]*protogen.Field, 0, len(o.annPath)+1)
	path = append(path, o.annPath...)
	return append(path, mustField(o.annMsg(), name))
}

// annGetter returns a nil-safe expression reading a field of recv's
// Annotations.
func (o *storeObject) annGetter(recv, name string) string {
	return getterChain(recv, o.annPathTo(name)...)
}

// annField returns an assignable path to a field of recv's Annotations.
func (o *storeObject) annField(recv, name string) string {
	return fieldChain(recv, o.annPathTo(name)...)
}

func (o *storeObject) genEventTypes(g *protogen.GeneratedFile) {
	n := o.name

	g.P("type ", n, "CheckFunc func(t1, t2 *", n, ") bool")
	g.P()

	// Event types implement two empty interfaces so that a consumer can filter
	// on either the object type or the change type:
	//
	//	type EventCreate interface { IsEventCreate() bool }
	//	type EventNode interface { IsEventNode() bool }
	//
	// The object type interface is generated once per object here; the change
	// type interfaces are hand-written in storeobject.go because they are only
	// needed once.
	g.P("type Event", n, " interface {")
	g.P("IsEvent", n, "() bool")
	g.P("}")
	g.P()

	event := g.QualifiedGoIdent(eventsPkg.Ident("Event"))

	for _, change := range []string{"Create", "Update", "Delete"} {
		g.P("type Event", change, n, " struct {")
		g.P(n, " *", n)
		if change == "Update" {
			g.P("Old", n, " *", n)
		}
		g.P("Checks []", n, "CheckFunc")
		g.P("}")
		g.P()

		g.P("func (e Event", change, n, ") Matches(apiEvent ", event, ") bool {")
		g.P("typedEvent, ok := apiEvent.(Event", change, n, ")")
		g.P("if !ok {")
		g.P("return false")
		g.P("}")
		g.P()
		g.P("for _, check := range e.Checks {")
		g.P("if !check(e.", n, ", typedEvent.", n, ") {")
		g.P("return false")
		g.P("}")
		g.P("}")
		g.P("return true")
		g.P("}")
		g.P()

		// Change type interface, e.g. IsEventCreate.
		g.P("func (e Event", change, n, ") IsEvent", change, "() bool {")
		g.P("return true")
		g.P("}")
		g.P()

		// Object type interface, e.g. IsEventNode.
		g.P("func (e Event", change, n, ") IsEvent", n, "() bool {")
		g.P("return true")
		g.P("}")
		g.P()
	}
}

func (o *storeObject) genStoreObjectMethods(g *protogen.GeneratedFile) {
	n := o.name
	meta := mustField(o.msg, "meta")

	g.P("func (m *", n, ") CopyStoreObject() StoreObject {")
	g.P("return m.Copy()")
	g.P("}")
	g.P()

	// StoreObject's GetId and GetMeta are already satisfied by the getters
	// protoc-gen-go emits for the id and meta fields, so declaring them here
	// would be a duplicate method. Only the setter is missing.
	g.P("func (m *", n, ") SetMeta(meta *", g.QualifiedGoIdent(meta.Message.GoIdent), ") {")
	g.P("m.", meta.GoName, " = meta")
	g.P("}")
	g.P()

	g.P("func (m *", n, ") EventCreate() Event {")
	g.P("return EventCreate", n, "{", n, ": m}")
	g.P("}")
	g.P()

	g.P("func (m *", n, ") EventUpdate(oldObject StoreObject) Event {")
	g.P("if oldObject != nil {")
	g.P("return EventUpdate", n, "{", n, ": m, Old", n, ": oldObject.(*", n, ")}")
	g.P("} else {")
	g.P("return EventUpdate", n, "{", n, ": m}")
	g.P("}")
	g.P("}")
	g.P()

	g.P("func (m *", n, ") EventDelete() Event {")
	g.P("return EventDelete", n, "{", n, ": m}")
	g.P("}")
	g.P()
}

// genCheckFuncs emits one comparison function per enabled watch selector. The
// functions are handed pairs of objects straight out of the store, so every hop
// through a nullable submessage goes through a getter.
func (o *storeObject) genCheckFuncs(g *protogen.GeneratedFile) {
	n, sel := o.name, o.sel

	check := func(suffix string, body func()) {
		g.P("func ", n, "Check", suffix, "(v1, v2 *", n, ") bool {")
		body()
		g.P("}")
		g.P()
	}

	// scalar emits a plain equality check on a top-level scalar field.
	scalar := func(suffix, protoName string) {
		f := mustField(o.msg, protoName)
		check(suffix, func() {
			g.P("return v1.", f.GoName, " == v2.", f.GoName)
		})
	}

	if sel.GetId() {
		scalar("ID", "id")
	}

	if sel.GetIdPrefix() {
		id := mustField(o.msg, "id")
		check("IDPrefix", func() {
			g.P("return ", g.QualifiedGoIdent(stringsPkg.Ident("HasPrefix")), "(v2.", id.GoName, ", v1.", id.GoName, ")")
		})
	}

	if sel.GetName() {
		check("Name", func() {
			if o.hostnamePath != nil {
				desc := o.hostnamePath[0]
				g.P("if v1.", desc.GoName, " == nil || v2.", desc.GoName, " == nil {")
				g.P("return false")
				g.P("}")
				g.P("return ", fieldChain("v1", o.hostnamePath...), " == ", fieldChain("v2", o.hostnamePath...))
				return
			}
			g.P("return ", o.annGetter("v1", "name"), " == ", o.annGetter("v2", "name"))
		})
	}

	if sel.GetNamePrefix() {
		hasPrefix := g.QualifiedGoIdent(stringsPkg.Ident("HasPrefix"))
		check("NamePrefix", func() {
			if o.hostnamePath != nil {
				desc := o.hostnamePath[0]
				g.P("if v1.", desc.GoName, " == nil || v2.", desc.GoName, " == nil {")
				g.P("return false")
				g.P("}")
				g.P("return ", hasPrefix, "(", fieldChain("v2", o.hostnamePath...), ", ", fieldChain("v1", o.hostnamePath...), ")")
				return
			}
			g.P("return ", hasPrefix, "(", o.annGetter("v2", "name"), ", ", o.annGetter("v1", "name"), ")")
		})
	}

	if sel.GetCustom() {
		check("Custom", func() {
			g.P("return checkCustom(", getterChain("v1", o.annPath...), ", ", getterChain("v2", o.annPath...), ")")
		})
	}

	if sel.GetCustomPrefix() {
		check("CustomPrefix", func() {
			g.P("return checkCustomPrefix(", getterChain("v1", o.annPath...), ", ", getterChain("v2", o.annPath...), ")")
		})
	}

	if sel.GetNodeId() {
		scalar("NodeID", "node_id")
	}
	if sel.GetServiceId() {
		scalar("ServiceID", "service_id")
	}
	if sel.GetSlot() {
		scalar("Slot", "slot")
	}
	if sel.GetDesiredState() {
		scalar("DesiredState", "desired_state")
	}
	if sel.GetRole() {
		scalar("Role", "role")
	}

	if sel.GetMembership() {
		spec := mustField(o.msg, "spec")
		membership := mustField(spec.Message, "membership")
		check("Membership", func() {
			g.P("return ", getterChain("v1", spec, membership), " == ", getterChain("v2", spec, membership))
		})
	}

	if sel.GetKind() {
		scalar("Kind", "kind")
	}
}

// genConvertWatch emits the Convert<Type>Watch function backing the watch API.
// It folds a list of SelectBy filters into a single prototype object plus the
// check functions comparing candidates against it.
func (o *storeObject) genConvertWatch(g *protogen.GeneratedFile) {
	n, sel := o.name, o.sel

	// The object addressed by kind (Resource) is the catch-all of the watch
	// API and needs the kind threaded through.
	if sel.GetKind() {
		g.P("func Convert", n, "Watch(action WatchActionKind, filters []*SelectBy, kind string) ([]Event, error) {")
	} else {
		g.P("func Convert", n, "Watch(action WatchActionKind, filters []*SelectBy) ([]Event, error) {")
	}

	g.P("var (")
	g.P("m ", n)
	g.P("checkFuncs []", n, "CheckFunc")
	// Enum selectors cannot use the zero value to detect a repeated filter,
	// because the zero value is a legitimate selection.
	if sel.GetDesiredState() {
		g.P("hasDesiredState bool")
	}
	if sel.GetRole() {
		g.P("hasRole bool")
	}
	if sel.GetMembership() {
		g.P("hasMembership bool")
	}
	g.P(")")

	// Submessages are nullable now, so the prototype needs its annotations
	// (and the spec holding them) allocated before a filter can write there.
	switch len(o.annPath) {
	case 1:
		ann := o.annPath[0]
		g.P("m.", ann.GoName, " = &", g.QualifiedGoIdent(ann.Message.GoIdent), "{}")
	case 2:
		spec, ann := o.annPath[0], o.annPath[1]
		g.P("m.", spec.GoName, " = &", g.QualifiedGoIdent(spec.Message.GoIdent), "{",
			ann.GoName, ": &", g.QualifiedGoIdent(ann.Message.GoIdent), "{}}")
	}

	if sel.GetKind() {
		g.P("m.", mustField(o.msg, "kind").GoName, " = kind")
		g.P("checkFuncs = append(checkFuncs, ", n, "CheckKind)")
	}
	g.P()
	g.P("for _, filter := range filters {")
	g.P("switch v := filter.By.(type) {")

	// conflict emits the guard rejecting a second filter of the same kind.
	conflict := func(cond string) {
		g.P("if ", cond, " {")
		g.P("return nil, errConflictingFilters")
		g.P("}")
	}
	appendChecks := func(suffixes ...string) {
		names := make([]string, 0, len(suffixes))
		for _, s := range suffixes {
			names = append(names, n+"Check"+s)
		}
		g.P("checkFuncs = append(checkFuncs, ", strings.Join(names, ", "), ")")
	}

	if sel.GetId() {
		id := mustField(o.msg, "id")
		g.P("case *SelectBy_Id:")
		conflict("m." + id.GoName + ` != ""`)
		g.P("m.", id.GoName, " = v.Id")
		appendChecks("ID")
	}
	if sel.GetIdPrefix() {
		id := mustField(o.msg, "id")
		g.P("case *SelectBy_IdPrefix:")
		conflict("m." + id.GoName + ` != ""`)
		g.P("m.", id.GoName, " = v.IdPrefix")
		appendChecks("IDPrefix")
	}
	if sel.GetName() {
		g.P("case *SelectBy_Name:")
		o.genNameFilter(g, "v.Name")
		appendChecks("Name")
	}
	if sel.GetNamePrefix() {
		g.P("case *SelectBy_NamePrefix:")
		o.genNameFilter(g, "v.NamePrefix")
		appendChecks("NamePrefix")
	}
	if sel.GetCustom() {
		g.P("case *SelectBy_Custom:")
		o.genCustomFilter(g, "v.Custom")
		appendChecks("Custom")
	}
	if sel.GetCustomPrefix() {
		g.P("case *SelectBy_CustomPrefix:")
		o.genCustomFilter(g, "v.CustomPrefix")
		appendChecks("CustomPrefix")
	}
	if sel.GetServiceId() {
		serviceID := mustField(o.msg, "service_id")
		g.P("case *SelectBy_ServiceId:")
		conflict("m." + serviceID.GoName + ` != ""`)
		g.P("m.", serviceID.GoName, " = v.ServiceId")
		appendChecks("ServiceID")
	}
	if sel.GetNodeId() {
		nodeID := mustField(o.msg, "node_id")
		g.P("case *SelectBy_NodeId:")
		conflict("m." + nodeID.GoName + ` != ""`)
		g.P("m.", nodeID.GoName, " = v.NodeId")
		appendChecks("NodeID")
	}
	if sel.GetSlot() {
		slot := mustField(o.msg, "slot")
		serviceID := mustField(o.msg, "service_id")
		g.P("case *SelectBy_Slot:")
		conflict("m." + slot.GoName + " != 0 || m." + serviceID.GoName + ` != ""`)
		g.P("m.", serviceID.GoName, " = v.Slot.ServiceId")
		g.P("m.", slot.GoName, " = v.Slot.Slot")
		// NOTE: CheckNodeID rather than CheckServiceID is what the gogo
		// generator emitted here; kept as-is to preserve watch behaviour.
		appendChecks("NodeID", "Slot")
	}
	if sel.GetDesiredState() {
		desiredState := mustField(o.msg, "desired_state")
		g.P("case *SelectBy_DesiredState:")
		conflict("hasDesiredState")
		g.P("hasDesiredState = true")
		g.P("m.", desiredState.GoName, " = v.DesiredState")
		appendChecks("DesiredState")
	}
	if sel.GetRole() {
		role := mustField(o.msg, "role")
		g.P("case *SelectBy_Role:")
		conflict("hasRole")
		g.P("hasRole = true")
		g.P("m.", role.GoName, " = v.Role")
		appendChecks("Role")
	}
	if sel.GetMembership() {
		spec := mustField(o.msg, "spec")
		membership := mustField(spec.Message, "membership")
		g.P("case *SelectBy_Membership:")
		conflict("hasMembership")
		g.P("hasMembership = true")
		g.P(fieldChain("m", spec, membership), " = v.Membership")
		appendChecks("Membership")
	}

	g.P("}")
	g.P("}")
	g.P("var events []Event")
	for _, ev := range []struct{ action, change string }{
		{watchActionCreate, "Create"},
		{watchActionUpdate, "Update"},
		{watchActionRemove, "Delete"},
	} {
		g.P("if (action & ", ev.action, ") != 0 {")
		g.P("events = append(events, Event", ev.change, n, "{", n, ": &m, Checks: checkFuncs})")
		g.P("}")
	}
	g.P("if len(events) == 0 {")
	g.P("return nil, errUnrecognizedAction")
	g.P("}")
	g.P("return events, nil")
	g.P("}")
	g.P()
}

// genNameFilter writes the name (or name prefix) held in expr into the
// prototype object.
func (o *storeObject) genNameFilter(g *protogen.GeneratedFile, expr string) {
	if o.hostnamePath != nil {
		desc, hostname := o.hostnamePath[0], o.hostnamePath[1]
		g.P("if m.", desc.GoName, " != nil {")
		g.P("return nil, errConflictingFilters")
		g.P("}")
		g.P("m.", desc.GoName, " = &", g.QualifiedGoIdent(desc.Message.GoIdent), "{", hostname.GoName, ": ", expr, "}")
		return
	}
	g.P("if ", o.annField("m", "name"), ` != "" {`)
	g.P("return nil, errConflictingFilters")
	g.P("}")
	g.P(o.annField("m", "name"), " = ", expr)
}

// genCustomFilter turns the SelectByCustom held in expr into a single index
// entry on the prototype object.
func (o *storeObject) genCustomFilter(g *protogen.GeneratedFile, expr string) {
	entry := mustField(o.annMsg(), "indices").Message
	g.P("if len(", o.annField("m", "indices"), ") != 0 {")
	g.P("return nil, errConflictingFilters")
	g.P("}")
	g.P(o.annField("m", "indices"), " = []*", g.QualifiedGoIdent(entry.GoIdent), "{{",
		mustField(entry, "key").GoName, ": ", expr, ".Index, ",
		mustField(entry, "val").GoName, ": ", expr, ".Value}}")
}

func (o *storeObject) genIndexers(g *protogen.GeneratedFile) {
	n := o.name

	g.P("type ", n, "IndexerByID struct{}")
	g.P()
	genFromArgs(g, n+"IndexerByID")
	genPrefixFromArgs(g, n+"IndexerByID")
	g.P("func (indexer ", n, "IndexerByID) FromObject(obj any) (bool, []byte, error) {")
	g.P("m := obj.(*", n, ")")
	// Add the null character as a terminator
	g.P(`return true, []byte(m.`, mustField(o.msg, "id").GoName, ` + "\x00"), nil`)
	g.P("}")

	g.P("type ", n, "IndexerByName struct{}")
	g.P()
	genFromArgs(g, n+"IndexerByName")
	genPrefixFromArgs(g, n+"IndexerByName")
	g.P("func (indexer ", n, "IndexerByName) FromObject(obj any) (bool, []byte, error) {")
	g.P("m := obj.(*", n, ")")
	g.P("val := ", o.annGetter("m", "name"))
	// Add the null character as a terminator
	g.P("return true, []byte(", g.QualifiedGoIdent(stringsPkg.Ident("ToLower")), `(val) + "\x00"), nil`)
	g.P("}")

	g.P("type ", n, "CustomIndexer struct{}")
	g.P()
	genFromArgs(g, n+"CustomIndexer")
	genPrefixFromArgs(g, n+"CustomIndexer")
	g.P("func (indexer ", n, "CustomIndexer) FromObject(obj any) (bool, [][]byte, error) {")
	g.P("m := obj.(*", n, ")")
	g.P(`return customIndexer("", `, getterChain("m", o.annPath...), ")")
	g.P("}")
}

func genFromArgs(g *protogen.GeneratedFile, indexerName string) {
	g.P("func (indexer ", indexerName, ") FromArgs(args ...any) ([]byte, error) {")
	g.P("return fromArgs(args...)")
	g.P("}")
}

func genPrefixFromArgs(g *protogen.GeneratedFile, indexerName string) {
	g.P("func (indexer ", indexerName, ") PrefixFromArgs(args ...any) ([]byte, error) {")
	g.P("return prefixFromArgs(args...)")
	g.P("}")
}

// genNewStoreAction emits the Event to StoreAction conversion used when
// proposing a transaction to raft.
func genNewStoreAction(g *protogen.GeneratedFile, objs []*storeObject) {
	// StoreAction is a protobuf message and must not be copied, so it is
	// returned by pointer; InternalRaftRequest.Action is a []*StoreAction too.
	g.P("func NewStoreAction(c Event) (*StoreAction, error) {")
	g.P("var sa StoreAction")
	g.P("switch v := c.(type) {")
	for _, o := range objs {
		n := o.name
		for _, ev := range []struct{ change, action string }{
			{"Create", storeActionCreate},
			{"Update", storeActionUpdate},
			{"Delete", storeActionRemove},
		} {
			g.P("case Event", ev.change, n, ":")
			g.P("sa.Action = ", ev.action)
			g.P("sa.Target = &StoreAction_", n, "{", n, ": v.", n, "}")
		}
	}
	g.P("default:")
	g.P("return nil, errUnknownStoreAction")
	g.P("}")
	g.P("return &sa, nil")
	g.P("}")
	g.P()
}

// genEventFromStoreAction emits the inverse of NewStoreAction, used when
// replaying the raft log into the store.
func genEventFromStoreAction(g *protogen.GeneratedFile, objs []*storeObject) {
	g.P("func EventFromStoreAction(sa *StoreAction, oldObject StoreObject) (Event, error) {")
	g.P("switch v := sa.GetTarget().(type) {")
	for _, o := range objs {
		n := o.name
		g.P("case *StoreAction_", n, ":")
		g.P("switch sa.GetAction() {")

		g.P("case ", storeActionCreate, ":")
		g.P("return EventCreate", n, "{", n, ": v.", n, "}, nil")

		g.P("case ", storeActionUpdate, ":")
		g.P("if oldObject != nil {")
		g.P("return EventUpdate", n, "{", n, ": v.", n, ", Old", n, ": oldObject.(*", n, ")}, nil")
		g.P("} else {")
		g.P("return EventUpdate", n, "{", n, ": v.", n, "}, nil")
		g.P("}")

		g.P("case ", storeActionRemove, ":")
		g.P("return EventDelete", n, "{", n, ": v.", n, "}, nil")

		g.P("}")
	}
	g.P("}")
	g.P("return nil, errUnknownStoreAction")
	g.P("}")
	g.P()
}

// genWatchMessageEvent emits the Event to wire message conversion for the watch
// API.
func genWatchMessageEvent(g *protogen.GeneratedFile, objs []*storeObject) {
	g.P("func WatchMessageEvent(c Event) *WatchMessage_Event {")
	g.P("switch v := c.(type) {")
	for _, o := range objs {
		n := o.name
		object := "&Object{Object: &Object_" + n + "{" + n + ": v." + n + "}}"
		oldObject := "&Object{Object: &Object_" + n + "{" + n + ": v.Old" + n + "}}"

		g.P("case EventCreate", n, ":")
		g.P("return &WatchMessage_Event{Action: ", watchActionCreate, ", Object: ", object, "}")

		g.P("case EventUpdate", n, ":")
		g.P("if v.Old", n, " != nil {")
		g.P("return &WatchMessage_Event{Action: ", watchActionUpdate, ", Object: ", object, ", OldObject: ", oldObject, "}")
		g.P("} else {")
		g.P("return &WatchMessage_Event{Action: ", watchActionUpdate, ", Object: ", object, "}")
		g.P("}")

		g.P("case EventDelete", n, ":")
		g.P("return &WatchMessage_Event{Action: ", watchActionRemove, ", Object: ", object, "}")
	}
	g.P("}")
	g.P("return nil")
	g.P("}")
	g.P()
}

// genConvertWatchArgs emits the dispatcher turning a watch request into the
// per-object matchers.
func genConvertWatchArgs(g *protogen.GeneratedFile, objs []*storeObject) {
	g.P("func ConvertWatchArgs(entries []*WatchRequest_WatchEntry) ([]Event, error) {")
	g.P("var events []Event")
	g.P("for _, entry := range entries {")
	g.P("var newEvents []Event")
	g.P("var err error")
	g.P("switch entry.Kind {")
	g.P(`case "":`)
	g.P("return nil, errNoKindSpecified")
	for _, o := range objs {
		n := o.name
		// The kind-addressed object is the fallback: any kind swarmkit does
		// not know natively is a resource of that kind.
		if o.sel.GetKind() {
			g.P("default:")
			g.P("newEvents, err = Convert", n, "Watch(entry.Action, entry.Filters, entry.Kind)")
			continue
		}
		g.P(`case "`, strings.ToLower(n), `":`)
		g.P("newEvents, err = Convert", n, "Watch(entry.Action, entry.Filters)")
	}
	g.P("}")
	g.P("if err != nil {")
	g.P("return nil, err")
	g.P("}")
	g.P("events = append(events, newEvents...)")
	g.P("}")
	g.P("return events, nil")
	g.P("}")
	g.P()
}
