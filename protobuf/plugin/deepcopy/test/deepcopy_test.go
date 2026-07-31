package test

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

// TestCopyIsDeep exercises the Copy methods emitted by protoc-gen-swarm across
// every field shape the generator has to handle: scalars, repeated scalars,
// nested messages, repeated messages, maps and oneofs.
//
// Copy delegates to the CloneVT methods from protoc-gen-go-vtproto, so what is
// really being guarded here is that the copy is independent of the original.
// Mutating any reference-typed field of the copy must not be observable
// through the source, which is what the store relies on to isolate the objects
// it hands out.
func TestCopyIsDeep(t *testing.T) {
	for _, tc := range []struct {
		name   string
		src    proto.Message
		copyFn func(proto.Message) proto.Message
		mutate func(proto.Message)
	}{
		{
			name: "BasicScalar",
			src:  &BasicScalar{Field3: 3, Field14: "fourteen", Field15: []byte("fifteen")},
			copyFn: func(m proto.Message) proto.Message {
				return m.(*BasicScalar).Copy()
			},
			mutate: func(m proto.Message) { m.(*BasicScalar).Field15[0] = 'X' },
		},
		{
			name: "RepeatedScalar",
			src:  &RepeatedScalar{Field3: []int32{1, 2, 3}, Field14: []string{"a", "b"}},
			copyFn: func(m proto.Message) proto.Message {
				return m.(*RepeatedScalar).Copy()
			},
			mutate: func(m proto.Message) { m.(*RepeatedScalar).Field3[0] = 99 },
		},
		{
			name: "ExternalStruct",
			src: &ExternalStruct{
				Field1: &BasicScalar{Field14: "nested"},
				Field2: &RepeatedScalar{Field3: []int32{7}},
			},
			copyFn: func(m proto.Message) proto.Message {
				return m.(*ExternalStruct).Copy()
			},
			mutate: func(m proto.Message) { m.(*ExternalStruct).Field1.Field14 = "mutated" },
		},
		{
			name: "RepeatedExternalStruct",
			src: &RepeatedExternalStruct{
				Field1: []*BasicScalar{{Field14: "one"}, {Field14: "two"}},
			},
			copyFn: func(m proto.Message) proto.Message {
				return m.(*RepeatedExternalStruct).Copy()
			},
			mutate: func(m proto.Message) { m.(*RepeatedExternalStruct).Field1[0].Field14 = "mutated" },
		},
		{
			name: "MapStruct",
			src: &MapStruct{
				NullableMap: map[string]*BasicScalar{"k": {Field14: "v"}},
			},
			copyFn: func(m proto.Message) proto.Message {
				return m.(*MapStruct).Copy()
			},
			mutate: func(m proto.Message) { m.(*MapStruct).NullableMap["k"].Field14 = "mutated" },
		},
		{
			name: "OneOfMessage",
			src: &OneOf{Fields: &OneOf_Field8{Field8: &MapStruct{
				NullableMap: map[string]*BasicScalar{"k": {Field14: "v"}},
			}}},
			copyFn: func(m proto.Message) proto.Message {
				return m.(*OneOf).Copy()
			},
			mutate: func(m proto.Message) {
				m.(*OneOf).Fields.(*OneOf_Field8).Field8.NullableMap["k"].Field14 = "mutated"
			},
		},
		{
			name: "OneOfScalar",
			src:  &OneOf{Fields: &OneOf_Field6{Field6: "six"}},
			copyFn: func(m proto.Message) proto.Message {
				return m.(*OneOf).Copy()
			},
			mutate: func(m proto.Message) { m.(*OneOf).Fields = &OneOf_Field6{Field6: "mutated"} },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cp := tc.copyFn(tc.src)
			if !proto.Equal(tc.src, cp) {
				t.Fatalf("Copy is not equal to the source\n src: %v\ncopy: %v", tc.src, cp)
			}

			// Keep a reference snapshot to compare the source against.
			before := proto.Clone(tc.src)
			tc.mutate(cp)

			if proto.Equal(tc.src, cp) {
				t.Error("mutating the copy did not change it; the test mutation is a no-op")
			}
			if !proto.Equal(tc.src, before) {
				t.Errorf("mutating the copy also changed the source, so Copy is shallow\n   src: %v\nbefore: %v", tc.src, before)
			}
		})
	}
}

// TestCopyFromReplaces checks that CopyFrom fully replaces the destination
// rather than merging into it. Copy is CloneVT, but CopyFrom is a separate
// implementation (proto.Reset followed by proto.Merge), and getting the reset
// wrong would leave stale repeated entries, map keys and oneof values behind.
func TestCopyFromReplaces(t *testing.T) {
	for _, tc := range []struct {
		name     string
		dst, src proto.Message
		copyFrom func(dst, src proto.Message)
	}{
		{
			name: "RepeatedIsReplacedNotAppended",
			dst:  &RepeatedScalar{Field3: []int32{9, 9, 9}, Field14: []string{"stale"}},
			src:  &RepeatedScalar{Field3: []int32{1}},
			copyFrom: func(dst, src proto.Message) {
				dst.(*RepeatedScalar).CopyFrom(src)
			},
		},
		{
			name: "MapKeysAreReplacedNotMerged",
			dst:  &MapStruct{NullableMap: map[string]*BasicScalar{"stale": {Field14: "old"}}},
			src:  &MapStruct{NullableMap: map[string]*BasicScalar{"fresh": {Field14: "new"}}},
			copyFrom: func(dst, src proto.Message) {
				dst.(*MapStruct).CopyFrom(src)
			},
		},
		{
			name: "OneofIsReplaced",
			dst:  &OneOf{Fields: &OneOf_Field6{Field6: "stale"}},
			src:  &OneOf{Fields: &OneOf_Field3{Field3: 7}},
			copyFrom: func(dst, src proto.Message) {
				dst.(*OneOf).CopyFrom(src)
			},
		},
		{
			name: "NestedMessageIsReplaced",
			dst:  &ExternalStruct{Field1: &BasicScalar{Field14: "stale", Field3: 5}},
			src:  &ExternalStruct{Field1: &BasicScalar{Field14: "new"}},
			copyFrom: func(dst, src proto.Message) {
				dst.(*ExternalStruct).CopyFrom(src)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := proto.Clone(tc.src)
			tc.copyFrom(tc.dst, tc.src)

			if !proto.Equal(tc.dst, want) {
				t.Errorf("CopyFrom did not replace the destination\n want: %v\n  got: %v", want, tc.dst)
			}

			// The source must not have been aliased into the destination.
			tc.copyFrom(tc.dst, tc.src)
			if !proto.Equal(tc.src, want) {
				t.Errorf("CopyFrom mutated the source\n want: %v\n  got: %v", want, tc.src)
			}
		})
	}
}

// TestCopyNil checks that Copy tolerates a nil receiver, which callers rely on
// when copying optional sub-messages.
func TestCopyNil(t *testing.T) {
	var m *ExternalStruct
	if cp := m.Copy(); cp != nil {
		t.Errorf("Copy of a nil message = %v, want nil", cp)
	}
}
