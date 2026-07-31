package test

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

func fullBasicScalar() *BasicScalar {
	return &BasicScalar{
		Field1:  1.5,
		Field2:  2.5,
		Field3:  3,
		Field4:  4,
		Field5:  5,
		Field6:  6,
		Field7:  7,
		Field8:  8,
		Field9:  9,
		Field10: 10,
		Field11: 11,
		Field12: 12,
		Field13: true,
		Field14: "fourteen",
		Field15: []byte("fifteen"),
	}
}

func fullRepeatedScalar() *RepeatedScalar {
	return &RepeatedScalar{
		Field1:  []float64{1, 2},
		Field2:  []float32{3, 4},
		Field3:  []int32{5, 6},
		Field4:  []int64{7, 8},
		Field5:  []uint32{9, 10},
		Field6:  []uint64{11, 12},
		Field7:  []int32{13, 14},
		Field8:  []int64{15, 16},
		Field9:  []uint32{17, 18},
		Field10: []int32{19, 20},
		Field11: []uint64{21, 22},
		Field12: []int64{23, 24},
		Field13: []bool{true, false},
		Field14: []string{"a", "b"},
		Field15: [][]byte{[]byte("x"), []byte("y")},
	}
}

func fullRepeatedScalarPacked() *RepeatedScalarPacked {
	return &RepeatedScalarPacked{
		Field1:  []float64{1, 2},
		Field2:  []float32{3, 4},
		Field3:  []int32{5, 6},
		Field4:  []int64{7, 8},
		Field5:  []uint32{9, 10},
		Field6:  []uint64{11, 12},
		Field7:  []int32{13, 14},
		Field8:  []int64{15, 16},
		Field9:  []uint32{17, 18},
		Field10: []int32{19, 20},
		Field11: []uint64{21, 22},
		Field12: []int64{23, 24},
		Field13: []bool{true, false},
	}
}

func fullExternalStruct() *ExternalStruct {
	return &ExternalStruct{
		Field1: fullBasicScalar(),
		Field2: fullRepeatedScalar(),
		Field3: fullRepeatedScalarPacked(),
	}
}

func fullRepeatedExternalStruct() *RepeatedExternalStruct {
	return &RepeatedExternalStruct{
		Field1: []*BasicScalar{fullBasicScalar(), fullBasicScalar()},
		Field2: []*RepeatedScalar{fullRepeatedScalar()},
		Field3: []*RepeatedScalarPacked{fullRepeatedScalarPacked()},
	}
}

func fullNonNullableExternalStruct() *NonNullableExternalStruct {
	return &NonNullableExternalStruct{
		Field1: fullBasicScalar(),
		Field2: fullRepeatedScalar(),
		Field3: fullRepeatedScalarPacked(),
	}
}

func fullRepeatedNonNullableExternalStruct() *RepeatedNonNullableExternalStruct {
	return &RepeatedNonNullableExternalStruct{
		Field1: []*BasicScalar{fullBasicScalar(), fullBasicScalar()},
		Field2: []*RepeatedScalar{fullRepeatedScalar()},
		Field3: []*RepeatedScalarPacked{fullRepeatedScalarPacked()},
	}
}

func fullMapStruct() *MapStruct {
	return &MapStruct{
		NullableMap:    map[string]*BasicScalar{"nullable": fullBasicScalar()},
		NonnullableMap: map[string]*BasicScalar{"nonnullable": fullBasicScalar()},
	}
}

func fullOneOf() *OneOf {
	return &OneOf{
		Fields:    &OneOf_Field8{Field8: fullMapStruct()},
		FieldsTwo: &OneOf_Field11{Field11: fullRepeatedExternalStruct()},
	}
}

type oneOfCase struct {
	name   string
	src    *OneOf
	mutate func(*OneOf)
}

func oneOfCases() []oneOfCase {
	return []oneOfCase{
		{
			name: "Field1",
			src:  &OneOf{Fields: &OneOf_Field1{Field1: 1.5}},
			mutate: func(m *OneOf) {
				m.Fields.(*OneOf_Field1).Field1 = 2.5
			},
		},
		{
			name: "Field2",
			src:  &OneOf{Fields: &OneOf_Field2{Field2: 2.5}},
			mutate: func(m *OneOf) {
				m.Fields.(*OneOf_Field2).Field2 = 3.5
			},
		},
		{
			name: "Field3",
			src:  &OneOf{Fields: &OneOf_Field3{Field3: 3}},
			mutate: func(m *OneOf) {
				m.Fields.(*OneOf_Field3).Field3 = 4
			},
		},
		{
			name: "Field4",
			src:  &OneOf{Fields: &OneOf_Field4{Field4: 4}},
			mutate: func(m *OneOf) {
				m.Fields.(*OneOf_Field4).Field4 = 5
			},
		},
		{
			name: "Field5",
			src:  &OneOf{Fields: &OneOf_Field5{Field5: 5}},
			mutate: func(m *OneOf) {
				m.Fields.(*OneOf_Field5).Field5 = 6
			},
		},
		{
			name: "Field6",
			src:  &OneOf{Fields: &OneOf_Field6{Field6: "six"}},
			mutate: func(m *OneOf) {
				m.Fields.(*OneOf_Field6).Field6 = "mutated"
			},
		},
		{
			name: "Field7",
			src:  &OneOf{Fields: &OneOf_Field7{Field7: []byte("seven")}},
			mutate: func(m *OneOf) {
				m.Fields.(*OneOf_Field7).Field7[0] = 'X'
			},
		},
		{
			name: "Field8",
			src: &OneOf{Fields: &OneOf_Field8{
				Field8: fullMapStruct(),
			}},
			mutate: func(m *OneOf) {
				m.Fields.(*OneOf_Field8).Field8.NullableMap["nullable"].Field14 = "mutated"
			},
		},
		{
			name: "Field9",
			src: &OneOf{Fields: &OneOf_Field9{
				Field9: fullRepeatedNonNullableExternalStruct(),
			}},
			mutate: func(m *OneOf) {
				m.Fields.(*OneOf_Field9).Field9.Field1[0].Field14 = "mutated"
			},
		},
		{
			name: "Field10",
			src: &OneOf{FieldsTwo: &OneOf_Field10{
				Field10: fullNonNullableExternalStruct(),
			}},
			mutate: func(m *OneOf) {
				m.FieldsTwo.(*OneOf_Field10).Field10.Field1.Field14 = "mutated"
			},
		},
		{
			name: "Field11",
			src: &OneOf{FieldsTwo: &OneOf_Field11{
				Field11: fullRepeatedExternalStruct(),
			}},
			mutate: func(m *OneOf) {
				m.FieldsTwo.(*OneOf_Field11).Field11.Field1[0].Field14 = "mutated"
			},
		},
	}
}

type copyCase struct {
	name   string
	src    proto.Message
	copyFn func(proto.Message) proto.Message
	mutate func(proto.Message)
}

func copyCases() []copyCase {
	cases := []copyCase{
		{
			name: "BasicScalar",
			src:  fullBasicScalar(),
			copyFn: func(m proto.Message) proto.Message {
				return m.(*BasicScalar).Copy()
			},
			mutate: func(m proto.Message) {
				m.(*BasicScalar).Field15[0] = 'X'
			},
		},
		{
			name: "RepeatedScalar",
			src:  fullRepeatedScalar(),
			copyFn: func(m proto.Message) proto.Message {
				return m.(*RepeatedScalar).Copy()
			},
			mutate: func(m proto.Message) {
				msg := m.(*RepeatedScalar)
				msg.Field3[0] = 99
				msg.Field15[0][0] = 'X'
			},
		},
		{
			name: "RepeatedScalarPacked",
			src:  fullRepeatedScalarPacked(),
			copyFn: func(m proto.Message) proto.Message {
				return m.(*RepeatedScalarPacked).Copy()
			},
			mutate: func(m proto.Message) {
				m.(*RepeatedScalarPacked).Field1[0] = 99
			},
		},
		{
			name: "ExternalStruct",
			src:  fullExternalStruct(),
			copyFn: func(m proto.Message) proto.Message {
				return m.(*ExternalStruct).Copy()
			},
			mutate: func(m proto.Message) {
				msg := m.(*ExternalStruct)
				msg.Field1.Field14 = "mutated"
				msg.Field2.Field3[0] = 99
				msg.Field3.Field1[0] = 99
			},
		},
		{
			name: "RepeatedExternalStruct",
			src:  fullRepeatedExternalStruct(),
			copyFn: func(m proto.Message) proto.Message {
				return m.(*RepeatedExternalStruct).Copy()
			},
			mutate: func(m proto.Message) {
				msg := m.(*RepeatedExternalStruct)
				msg.Field1[0].Field14 = "mutated"
				msg.Field2[0].Field3[0] = 99
				msg.Field3[0].Field1[0] = 99
			},
		},
		{
			name: "NonNullableExternalStruct",
			src:  fullNonNullableExternalStruct(),
			copyFn: func(m proto.Message) proto.Message {
				return m.(*NonNullableExternalStruct).Copy()
			},
			mutate: func(m proto.Message) {
				msg := m.(*NonNullableExternalStruct)
				msg.Field1.Field14 = "mutated"
				msg.Field2.Field3[0] = 99
				msg.Field3.Field1[0] = 99
			},
		},
		{
			name: "RepeatedNonNullableExternalStruct",
			src:  fullRepeatedNonNullableExternalStruct(),
			copyFn: func(m proto.Message) proto.Message {
				return m.(*RepeatedNonNullableExternalStruct).Copy()
			},
			mutate: func(m proto.Message) {
				msg := m.(*RepeatedNonNullableExternalStruct)
				msg.Field1[0].Field14 = "mutated"
				msg.Field2[0].Field3[0] = 99
				msg.Field3[0].Field1[0] = 99
			},
		},
		{
			name: "MapStruct",
			src:  fullMapStruct(),
			copyFn: func(m proto.Message) proto.Message {
				return m.(*MapStruct).Copy()
			},
			mutate: func(m proto.Message) {
				msg := m.(*MapStruct)
				msg.NullableMap["nullable"].Field14 = "mutated"
				msg.NullableMap["added"] = &BasicScalar{Field14: "added"}
				msg.NonnullableMap["nonnullable"].Field14 = "mutated"
				msg.NonnullableMap["added"] = &BasicScalar{Field14: "added"}
			},
		},
		{
			name: "OneOf",
			src:  fullOneOf(),
			copyFn: func(m proto.Message) proto.Message {
				return m.(*OneOf).Copy()
			},
			mutate: func(m proto.Message) {
				msg := m.(*OneOf)
				msg.GetField8().NullableMap["nullable"].Field14 = "mutated"
				msg.GetField11().Field1[0].Field14 = "mutated"
			},
		},
	}

	for _, tc := range oneOfCases() {
		tc := tc
		cases = append(cases, copyCase{
			name: "OneOf/" + tc.name,
			src:  tc.src,
			copyFn: func(m proto.Message) proto.Message {
				return m.(*OneOf).Copy()
			},
			mutate: func(m proto.Message) {
				tc.mutate(m.(*OneOf))
			},
		})
	}
	return cases
}

// TestCopyIsDeep exercises the Copy methods emitted by protoc-gen-swarm across
// all field shapes the generator has to handle: scalars, repeated scalars,
// nested messages, repeated messages, maps and oneofs.
//
// Mutating any reference-typed field of the copy must not be observable through
// the source, which is what the store relies on to isolate the objects it hands
// out.
func TestCopyIsDeep(t *testing.T) {
	for _, tc := range copyCases() {
		t.Run(tc.name, func(t *testing.T) {
			before := proto.Clone(tc.src)
			cp := tc.copyFn(tc.src)
			if !proto.Equal(tc.src, cp) {
				t.Fatalf("Copy is not equal to the source\n src: %v\ncopy: %v", tc.src, cp)
			}

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

// TestCopyFromReplaces checks that CopyFrom fully replaces the destination rather
// than merging into it.
// It also verifies that mutating the destination after a copy never changes the
// source.
func TestCopyFromReplaces(t *testing.T) {
	cases := []struct {
		name     string
		dst, src proto.Message
		copyFrom func(dst, src proto.Message)
		mutate   func(proto.Message)
	}{
		{
			name: "BasicScalar",
			dst:  &BasicScalar{Field14: "stale", Field15: []byte("stale")},
			src:  fullBasicScalar(),
			copyFrom: func(dst, src proto.Message) {
				dst.(*BasicScalar).CopyFrom(src)
			},
			mutate: func(m proto.Message) {
				m.(*BasicScalar).Field15[0] = 'X'
			},
		},
		{
			name: "RepeatedScalar",
			dst:  &RepeatedScalar{Field3: []int32{9}, Field15: [][]byte{[]byte("stale")}},
			src:  fullRepeatedScalar(),
			copyFrom: func(dst, src proto.Message) {
				dst.(*RepeatedScalar).CopyFrom(src)
			},
			mutate: func(m proto.Message) {
				msg := m.(*RepeatedScalar)
				msg.Field3[0] = 99
				msg.Field15[0][0] = 'X'
			},
		},
		{
			name: "RepeatedScalarPacked",
			dst:  &RepeatedScalarPacked{Field1: []float64{9}},
			src:  fullRepeatedScalarPacked(),
			copyFrom: func(dst, src proto.Message) {
				dst.(*RepeatedScalarPacked).CopyFrom(src)
			},
			mutate: func(m proto.Message) {
				m.(*RepeatedScalarPacked).Field1[0] = 99
			},
		},
		{
			name: "ExternalStruct",
			dst:  &ExternalStruct{Field1: &BasicScalar{Field14: "stale"}},
			src:  fullExternalStruct(),
			copyFrom: func(dst, src proto.Message) {
				dst.(*ExternalStruct).CopyFrom(src)
			},
			mutate: func(m proto.Message) {
				m.(*ExternalStruct).Field1.Field14 = "mutated"
			},
		},
		{
			name: "RepeatedExternalStruct",
			dst:  &RepeatedExternalStruct{Field1: []*BasicScalar{{Field14: "stale"}}},
			src:  fullRepeatedExternalStruct(),
			copyFrom: func(dst, src proto.Message) {
				dst.(*RepeatedExternalStruct).CopyFrom(src)
			},
			mutate: func(m proto.Message) {
				m.(*RepeatedExternalStruct).Field1[0].Field14 = "mutated"
			},
		},
		{
			name: "NonNullableExternalStruct",
			dst:  &NonNullableExternalStruct{Field1: &BasicScalar{Field14: "stale"}},
			src:  fullNonNullableExternalStruct(),
			copyFrom: func(dst, src proto.Message) {
				dst.(*NonNullableExternalStruct).CopyFrom(src)
			},
			mutate: func(m proto.Message) {
				m.(*NonNullableExternalStruct).Field1.Field14 = "mutated"
			},
		},
		{
			name: "RepeatedNonNullableExternalStruct",
			dst: &RepeatedNonNullableExternalStruct{
				Field1: []*BasicScalar{{Field14: "stale"}},
			},
			src: fullRepeatedNonNullableExternalStruct(),
			copyFrom: func(dst, src proto.Message) {
				dst.(*RepeatedNonNullableExternalStruct).CopyFrom(src)
			},
			mutate: func(m proto.Message) {
				m.(*RepeatedNonNullableExternalStruct).Field1[0].Field14 = "mutated"
			},
		},
		{
			name: "MapKeysAreReplacedNotMerged",
			dst: &MapStruct{
				NullableMap:    map[string]*BasicScalar{"stale": {Field14: "old"}},
				NonnullableMap: map[string]*BasicScalar{"stale": {Field14: "old"}},
			},
			src: fullMapStruct(),
			copyFrom: func(dst, src proto.Message) {
				dst.(*MapStruct).CopyFrom(src)
			},
			mutate: func(m proto.Message) {
				msg := m.(*MapStruct)
				msg.NullableMap["nullable"].Field14 = "mutated"
				msg.NullableMap["added"] = &BasicScalar{Field14: "added"}
				msg.NonnullableMap["nonnullable"].Field14 = "mutated"
				msg.NonnullableMap["added"] = &BasicScalar{Field14: "added"}
			},
		},
		{
			name: "OneofIsReplaced",
			dst: &OneOf{
				Fields:    &OneOf_Field3{Field3: 9},
				FieldsTwo: &OneOf_Field10{Field10: &NonNullableExternalStruct{Field1: &BasicScalar{Field14: "stale"}}},
			},
			src: &OneOf{
				Fields:    &OneOf_Field6{Field6: "fresh"},
				FieldsTwo: &OneOf_Field11{Field11: fullRepeatedExternalStruct()},
			},
			copyFrom: func(dst, src proto.Message) {
				dst.(*OneOf).CopyFrom(src)
			},
			mutate: func(m proto.Message) {
				m.(*OneOf).Fields.(*OneOf_Field6).Field6 = "mutated"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := proto.Clone(tc.src)
			tc.copyFrom(tc.dst, tc.src)

			if !proto.Equal(tc.dst, want) {
				t.Errorf("CopyFrom did not replace the destination\n want: %v\n  got: %v", want, tc.dst)
			}

			tc.mutate(tc.dst)
			if proto.Equal(tc.dst, want) {
				t.Error("mutating the destination did not change it; the test mutation is a no-op")
			}
			if !proto.Equal(tc.src, want) {
				t.Errorf("mutating the destination also changed the source, so CopyFrom is shallow\n   src: %v\nbefore: %v", tc.src, want)
			}

			// CopyFrom must still replace the mutated destination on a subsequent copy.
			tc.copyFrom(tc.dst, tc.src)
			if !proto.Equal(tc.dst, want) {
				t.Errorf("CopyFrom did not replace the mutated destination\n want: %v\n  got: %v", want, tc.dst)
			}
		})
	}
}

func TestProtoRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		src  proto.Message
		dst  func() proto.Message
	}{
		{
			name: "BasicScalar",
			src:  fullBasicScalar(),
			dst:  func() proto.Message { return &BasicScalar{} },
		},
		{
			name: "RepeatedScalar",
			src:  fullRepeatedScalar(),
			dst:  func() proto.Message { return &RepeatedScalar{} },
		},
		{
			name: "RepeatedScalarPacked",
			src:  fullRepeatedScalarPacked(),
			dst:  func() proto.Message { return &RepeatedScalarPacked{} },
		},
		{
			name: "ExternalStruct",
			src:  fullExternalStruct(),
			dst:  func() proto.Message { return &ExternalStruct{} },
		},
		{
			name: "RepeatedExternalStruct",
			src:  fullRepeatedExternalStruct(),
			dst:  func() proto.Message { return &RepeatedExternalStruct{} },
		},
		{
			name: "NonNullableExternalStruct",
			src:  fullNonNullableExternalStruct(),
			dst:  func() proto.Message { return &NonNullableExternalStruct{} },
		},
		{
			name: "RepeatedNonNullableExternalStruct",
			src:  fullRepeatedNonNullableExternalStruct(),
			dst:  func() proto.Message { return &RepeatedNonNullableExternalStruct{} },
		},
		{
			name: "MapStruct",
			src:  fullMapStruct(),
			dst:  func() proto.Message { return &MapStruct{} },
		},
		{
			name: "OneOf",
			src:  fullOneOf(),
			dst:  func() proto.Message { return &OneOf{} },
		},
	}
	for _, tc := range oneOfCases() {
		tc := tc
		cases = append(cases, struct {
			name string
			src  proto.Message
			dst  func() proto.Message
		}{
			name: "OneOf/" + tc.name,
			src:  tc.src,
			dst:  func() proto.Message { return &OneOf{} },
		})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := proto.Marshal(tc.src)
			if err != nil {
				t.Fatalf("proto.Marshal failed: %v", err)
			}

			got := tc.dst()
			if err := proto.Unmarshal(data, got); err != nil {
				t.Fatalf("proto.Unmarshal failed: %v", err)
			}
			if !proto.Equal(tc.src, got) {
				t.Errorf("round trip changed the message\n want: %v\n  got: %v", tc.src, got)
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
