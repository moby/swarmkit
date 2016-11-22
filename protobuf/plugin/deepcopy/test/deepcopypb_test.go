// Code generated by protoc-gen-gogo.
// source: deepcopy.proto
// DO NOT EDIT!

/*
Package test is a generated protocol buffer package.

It is generated from these files:
	deepcopy.proto

It has these top-level messages:
	BasicScalar
	RepeatedScalar
	RepeatedScalarPacked
	ExternalStruct
	RepeatedExternalStruct
	NonNullableExternalStruct
	RepeatedNonNullableExternalStruct
	MapStruct
	OneOf
*/
package test

import testing "testing"
import math_rand "math/rand"
import time "time"
import github_com_gogo_protobuf_proto "github.com/gogo/protobuf/proto"
import github_com_gogo_protobuf_jsonpb "github.com/gogo/protobuf/jsonpb"
import fmt "fmt"
import proto "github.com/gogo/protobuf/proto"
import math "math"
import _ "github.com/gogo/protobuf/gogoproto"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

func TestBasicScalarProto(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedBasicScalar(popr, false)
	dAtA, err := github_com_gogo_protobuf_proto.Marshal(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &BasicScalar{}
	if err := github_com_gogo_protobuf_proto.Unmarshal(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	littlefuzz := make([]byte, len(dAtA))
	copy(littlefuzz, dAtA)
	for i := range dAtA {
		dAtA[i] = byte(popr.Intn(256))
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
	if len(littlefuzz) > 0 {
		fuzzamount := 100
		for i := 0; i < fuzzamount; i++ {
			littlefuzz[popr.Intn(len(littlefuzz))] = byte(popr.Intn(256))
			littlefuzz = append(littlefuzz, byte(popr.Intn(256)))
		}
		// shouldn't panic
		_ = github_com_gogo_protobuf_proto.Unmarshal(littlefuzz, msg)
	}
}

func TestRepeatedScalarProto(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedRepeatedScalar(popr, false)
	dAtA, err := github_com_gogo_protobuf_proto.Marshal(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &RepeatedScalar{}
	if err := github_com_gogo_protobuf_proto.Unmarshal(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	littlefuzz := make([]byte, len(dAtA))
	copy(littlefuzz, dAtA)
	for i := range dAtA {
		dAtA[i] = byte(popr.Intn(256))
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
	if len(littlefuzz) > 0 {
		fuzzamount := 100
		for i := 0; i < fuzzamount; i++ {
			littlefuzz[popr.Intn(len(littlefuzz))] = byte(popr.Intn(256))
			littlefuzz = append(littlefuzz, byte(popr.Intn(256)))
		}
		// shouldn't panic
		_ = github_com_gogo_protobuf_proto.Unmarshal(littlefuzz, msg)
	}
}

func TestRepeatedScalarPackedProto(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedRepeatedScalarPacked(popr, false)
	dAtA, err := github_com_gogo_protobuf_proto.Marshal(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &RepeatedScalarPacked{}
	if err := github_com_gogo_protobuf_proto.Unmarshal(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	littlefuzz := make([]byte, len(dAtA))
	copy(littlefuzz, dAtA)
	for i := range dAtA {
		dAtA[i] = byte(popr.Intn(256))
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
	if len(littlefuzz) > 0 {
		fuzzamount := 100
		for i := 0; i < fuzzamount; i++ {
			littlefuzz[popr.Intn(len(littlefuzz))] = byte(popr.Intn(256))
			littlefuzz = append(littlefuzz, byte(popr.Intn(256)))
		}
		// shouldn't panic
		_ = github_com_gogo_protobuf_proto.Unmarshal(littlefuzz, msg)
	}
}

func TestExternalStructProto(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedExternalStruct(popr, false)
	dAtA, err := github_com_gogo_protobuf_proto.Marshal(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &ExternalStruct{}
	if err := github_com_gogo_protobuf_proto.Unmarshal(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	littlefuzz := make([]byte, len(dAtA))
	copy(littlefuzz, dAtA)
	for i := range dAtA {
		dAtA[i] = byte(popr.Intn(256))
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
	if len(littlefuzz) > 0 {
		fuzzamount := 100
		for i := 0; i < fuzzamount; i++ {
			littlefuzz[popr.Intn(len(littlefuzz))] = byte(popr.Intn(256))
			littlefuzz = append(littlefuzz, byte(popr.Intn(256)))
		}
		// shouldn't panic
		_ = github_com_gogo_protobuf_proto.Unmarshal(littlefuzz, msg)
	}
}

func TestRepeatedExternalStructProto(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedRepeatedExternalStruct(popr, false)
	dAtA, err := github_com_gogo_protobuf_proto.Marshal(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &RepeatedExternalStruct{}
	if err := github_com_gogo_protobuf_proto.Unmarshal(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	littlefuzz := make([]byte, len(dAtA))
	copy(littlefuzz, dAtA)
	for i := range dAtA {
		dAtA[i] = byte(popr.Intn(256))
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
	if len(littlefuzz) > 0 {
		fuzzamount := 100
		for i := 0; i < fuzzamount; i++ {
			littlefuzz[popr.Intn(len(littlefuzz))] = byte(popr.Intn(256))
			littlefuzz = append(littlefuzz, byte(popr.Intn(256)))
		}
		// shouldn't panic
		_ = github_com_gogo_protobuf_proto.Unmarshal(littlefuzz, msg)
	}
}

func TestNonNullableExternalStructProto(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedNonNullableExternalStruct(popr, false)
	dAtA, err := github_com_gogo_protobuf_proto.Marshal(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &NonNullableExternalStruct{}
	if err := github_com_gogo_protobuf_proto.Unmarshal(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	littlefuzz := make([]byte, len(dAtA))
	copy(littlefuzz, dAtA)
	for i := range dAtA {
		dAtA[i] = byte(popr.Intn(256))
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
	if len(littlefuzz) > 0 {
		fuzzamount := 100
		for i := 0; i < fuzzamount; i++ {
			littlefuzz[popr.Intn(len(littlefuzz))] = byte(popr.Intn(256))
			littlefuzz = append(littlefuzz, byte(popr.Intn(256)))
		}
		// shouldn't panic
		_ = github_com_gogo_protobuf_proto.Unmarshal(littlefuzz, msg)
	}
}

func TestRepeatedNonNullableExternalStructProto(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedRepeatedNonNullableExternalStruct(popr, false)
	dAtA, err := github_com_gogo_protobuf_proto.Marshal(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &RepeatedNonNullableExternalStruct{}
	if err := github_com_gogo_protobuf_proto.Unmarshal(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	littlefuzz := make([]byte, len(dAtA))
	copy(littlefuzz, dAtA)
	for i := range dAtA {
		dAtA[i] = byte(popr.Intn(256))
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
	if len(littlefuzz) > 0 {
		fuzzamount := 100
		for i := 0; i < fuzzamount; i++ {
			littlefuzz[popr.Intn(len(littlefuzz))] = byte(popr.Intn(256))
			littlefuzz = append(littlefuzz, byte(popr.Intn(256)))
		}
		// shouldn't panic
		_ = github_com_gogo_protobuf_proto.Unmarshal(littlefuzz, msg)
	}
}

func TestMapStructProto(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedMapStruct(popr, false)
	dAtA, err := github_com_gogo_protobuf_proto.Marshal(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &MapStruct{}
	if err := github_com_gogo_protobuf_proto.Unmarshal(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	littlefuzz := make([]byte, len(dAtA))
	copy(littlefuzz, dAtA)
	for i := range dAtA {
		dAtA[i] = byte(popr.Intn(256))
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
	if len(littlefuzz) > 0 {
		fuzzamount := 100
		for i := 0; i < fuzzamount; i++ {
			littlefuzz[popr.Intn(len(littlefuzz))] = byte(popr.Intn(256))
			littlefuzz = append(littlefuzz, byte(popr.Intn(256)))
		}
		// shouldn't panic
		_ = github_com_gogo_protobuf_proto.Unmarshal(littlefuzz, msg)
	}
}

func TestOneOfProto(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedOneOf(popr, false)
	dAtA, err := github_com_gogo_protobuf_proto.Marshal(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &OneOf{}
	if err := github_com_gogo_protobuf_proto.Unmarshal(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	littlefuzz := make([]byte, len(dAtA))
	copy(littlefuzz, dAtA)
	for i := range dAtA {
		dAtA[i] = byte(popr.Intn(256))
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
	if len(littlefuzz) > 0 {
		fuzzamount := 100
		for i := 0; i < fuzzamount; i++ {
			littlefuzz[popr.Intn(len(littlefuzz))] = byte(popr.Intn(256))
			littlefuzz = append(littlefuzz, byte(popr.Intn(256)))
		}
		// shouldn't panic
		_ = github_com_gogo_protobuf_proto.Unmarshal(littlefuzz, msg)
	}
}

func TestBasicScalarJSON(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedBasicScalar(popr, true)
	marshaler := github_com_gogo_protobuf_jsonpb.Marshaler{}
	jsondata, err := marshaler.MarshalToString(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &BasicScalar{}
	err = github_com_gogo_protobuf_jsonpb.UnmarshalString(jsondata, msg)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Json Equal %#v", seed, msg, p)
	}
}
func TestRepeatedScalarJSON(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedRepeatedScalar(popr, true)
	marshaler := github_com_gogo_protobuf_jsonpb.Marshaler{}
	jsondata, err := marshaler.MarshalToString(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &RepeatedScalar{}
	err = github_com_gogo_protobuf_jsonpb.UnmarshalString(jsondata, msg)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Json Equal %#v", seed, msg, p)
	}
}
func TestRepeatedScalarPackedJSON(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedRepeatedScalarPacked(popr, true)
	marshaler := github_com_gogo_protobuf_jsonpb.Marshaler{}
	jsondata, err := marshaler.MarshalToString(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &RepeatedScalarPacked{}
	err = github_com_gogo_protobuf_jsonpb.UnmarshalString(jsondata, msg)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Json Equal %#v", seed, msg, p)
	}
}
func TestExternalStructJSON(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedExternalStruct(popr, true)
	marshaler := github_com_gogo_protobuf_jsonpb.Marshaler{}
	jsondata, err := marshaler.MarshalToString(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &ExternalStruct{}
	err = github_com_gogo_protobuf_jsonpb.UnmarshalString(jsondata, msg)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Json Equal %#v", seed, msg, p)
	}
}
func TestRepeatedExternalStructJSON(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedRepeatedExternalStruct(popr, true)
	marshaler := github_com_gogo_protobuf_jsonpb.Marshaler{}
	jsondata, err := marshaler.MarshalToString(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &RepeatedExternalStruct{}
	err = github_com_gogo_protobuf_jsonpb.UnmarshalString(jsondata, msg)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Json Equal %#v", seed, msg, p)
	}
}
func TestNonNullableExternalStructJSON(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedNonNullableExternalStruct(popr, true)
	marshaler := github_com_gogo_protobuf_jsonpb.Marshaler{}
	jsondata, err := marshaler.MarshalToString(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &NonNullableExternalStruct{}
	err = github_com_gogo_protobuf_jsonpb.UnmarshalString(jsondata, msg)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Json Equal %#v", seed, msg, p)
	}
}
func TestRepeatedNonNullableExternalStructJSON(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedRepeatedNonNullableExternalStruct(popr, true)
	marshaler := github_com_gogo_protobuf_jsonpb.Marshaler{}
	jsondata, err := marshaler.MarshalToString(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &RepeatedNonNullableExternalStruct{}
	err = github_com_gogo_protobuf_jsonpb.UnmarshalString(jsondata, msg)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Json Equal %#v", seed, msg, p)
	}
}
func TestMapStructJSON(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedMapStruct(popr, true)
	marshaler := github_com_gogo_protobuf_jsonpb.Marshaler{}
	jsondata, err := marshaler.MarshalToString(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &MapStruct{}
	err = github_com_gogo_protobuf_jsonpb.UnmarshalString(jsondata, msg)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Json Equal %#v", seed, msg, p)
	}
}
func TestOneOfJSON(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedOneOf(popr, true)
	marshaler := github_com_gogo_protobuf_jsonpb.Marshaler{}
	jsondata, err := marshaler.MarshalToString(p)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	msg := &OneOf{}
	err = github_com_gogo_protobuf_jsonpb.UnmarshalString(jsondata, msg)
	if err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Json Equal %#v", seed, msg, p)
	}
}
func TestBasicScalarProtoText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedBasicScalar(popr, true)
	dAtA := github_com_gogo_protobuf_proto.MarshalTextString(p)
	msg := &BasicScalar{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestBasicScalarProtoCompactText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedBasicScalar(popr, true)
	dAtA := github_com_gogo_protobuf_proto.CompactTextString(p)
	msg := &BasicScalar{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestRepeatedScalarProtoText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedRepeatedScalar(popr, true)
	dAtA := github_com_gogo_protobuf_proto.MarshalTextString(p)
	msg := &RepeatedScalar{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestRepeatedScalarProtoCompactText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedRepeatedScalar(popr, true)
	dAtA := github_com_gogo_protobuf_proto.CompactTextString(p)
	msg := &RepeatedScalar{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestRepeatedScalarPackedProtoText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedRepeatedScalarPacked(popr, true)
	dAtA := github_com_gogo_protobuf_proto.MarshalTextString(p)
	msg := &RepeatedScalarPacked{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestRepeatedScalarPackedProtoCompactText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedRepeatedScalarPacked(popr, true)
	dAtA := github_com_gogo_protobuf_proto.CompactTextString(p)
	msg := &RepeatedScalarPacked{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestExternalStructProtoText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedExternalStruct(popr, true)
	dAtA := github_com_gogo_protobuf_proto.MarshalTextString(p)
	msg := &ExternalStruct{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestExternalStructProtoCompactText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedExternalStruct(popr, true)
	dAtA := github_com_gogo_protobuf_proto.CompactTextString(p)
	msg := &ExternalStruct{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestRepeatedExternalStructProtoText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedRepeatedExternalStruct(popr, true)
	dAtA := github_com_gogo_protobuf_proto.MarshalTextString(p)
	msg := &RepeatedExternalStruct{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestRepeatedExternalStructProtoCompactText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedRepeatedExternalStruct(popr, true)
	dAtA := github_com_gogo_protobuf_proto.CompactTextString(p)
	msg := &RepeatedExternalStruct{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestNonNullableExternalStructProtoText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedNonNullableExternalStruct(popr, true)
	dAtA := github_com_gogo_protobuf_proto.MarshalTextString(p)
	msg := &NonNullableExternalStruct{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestNonNullableExternalStructProtoCompactText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedNonNullableExternalStruct(popr, true)
	dAtA := github_com_gogo_protobuf_proto.CompactTextString(p)
	msg := &NonNullableExternalStruct{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestRepeatedNonNullableExternalStructProtoText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedRepeatedNonNullableExternalStruct(popr, true)
	dAtA := github_com_gogo_protobuf_proto.MarshalTextString(p)
	msg := &RepeatedNonNullableExternalStruct{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestRepeatedNonNullableExternalStructProtoCompactText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedRepeatedNonNullableExternalStruct(popr, true)
	dAtA := github_com_gogo_protobuf_proto.CompactTextString(p)
	msg := &RepeatedNonNullableExternalStruct{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestMapStructProtoText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedMapStruct(popr, true)
	dAtA := github_com_gogo_protobuf_proto.MarshalTextString(p)
	msg := &MapStruct{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestMapStructProtoCompactText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedMapStruct(popr, true)
	dAtA := github_com_gogo_protobuf_proto.CompactTextString(p)
	msg := &MapStruct{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestOneOfProtoText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedOneOf(popr, true)
	dAtA := github_com_gogo_protobuf_proto.MarshalTextString(p)
	msg := &OneOf{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestOneOfProtoCompactText(t *testing.T) {
	seed := time.Now().UnixNano()
	popr := math_rand.New(math_rand.NewSource(seed))
	p := NewPopulatedOneOf(popr, true)
	dAtA := github_com_gogo_protobuf_proto.CompactTextString(p)
	msg := &OneOf{}
	if err := github_com_gogo_protobuf_proto.UnmarshalText(dAtA, msg); err != nil {
		t.Fatalf("seed = %d, err = %v", seed, err)
	}
	if !p.Equal(msg) {
		t.Fatalf("seed = %d, %#v !Proto %#v", seed, msg, p)
	}
}

func TestBasicScalarCopy(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	in := NewPopulatedBasicScalar(popr, true)
	out := in.Copy()
	if !in.Equal(out) {
		t.Fatalf("%#v != %#v", in, out)
	}
	if &in.Field1 == &out.Field1 {
		t.Fatalf("Field1: %#v == %#v", &in.Field1, &out.Field1)
	}
	if &in.Field2 == &out.Field2 {
		t.Fatalf("Field2: %#v == %#v", &in.Field2, &out.Field2)
	}
	if &in.Field3 == &out.Field3 {
		t.Fatalf("Field3: %#v == %#v", &in.Field3, &out.Field3)
	}
	if &in.Field4 == &out.Field4 {
		t.Fatalf("Field4: %#v == %#v", &in.Field4, &out.Field4)
	}
	if &in.Field5 == &out.Field5 {
		t.Fatalf("Field5: %#v == %#v", &in.Field5, &out.Field5)
	}
	if &in.Field6 == &out.Field6 {
		t.Fatalf("Field6: %#v == %#v", &in.Field6, &out.Field6)
	}
	if &in.Field7 == &out.Field7 {
		t.Fatalf("Field7: %#v == %#v", &in.Field7, &out.Field7)
	}
	if &in.Field8 == &out.Field8 {
		t.Fatalf("Field8: %#v == %#v", &in.Field8, &out.Field8)
	}
	if &in.Field9 == &out.Field9 {
		t.Fatalf("Field9: %#v == %#v", &in.Field9, &out.Field9)
	}
	if &in.Field10 == &out.Field10 {
		t.Fatalf("Field10: %#v == %#v", &in.Field10, &out.Field10)
	}
	if &in.Field11 == &out.Field11 {
		t.Fatalf("Field11: %#v == %#v", &in.Field11, &out.Field11)
	}
	if &in.Field12 == &out.Field12 {
		t.Fatalf("Field12: %#v == %#v", &in.Field12, &out.Field12)
	}
	if &in.Field13 == &out.Field13 {
		t.Fatalf("Field13: %#v == %#v", &in.Field13, &out.Field13)
	}
	if &in.Field14 == &out.Field14 {
		t.Fatalf("Field14: %#v == %#v", &in.Field14, &out.Field14)
	}
	if &in.Field15 == &out.Field15 {
		t.Fatalf("Field15: %#v == %#v", &in.Field15, &out.Field15)
	}

	in = nil
	out = in.Copy()
	if out != nil {
		t.Fatalf("copying nil should return nil, returned: %#v", out)
	}
}

func TestRepeatedScalarCopy(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	in := NewPopulatedRepeatedScalar(popr, true)
	out := in.Copy()
	if !in.Equal(out) {
		t.Fatalf("%#v != %#v", in, out)
	}
	if &in.Field1 == &out.Field1 {
		t.Fatalf("Field1: %#v == %#v", &in.Field1, &out.Field1)
	}
	if &in.Field2 == &out.Field2 {
		t.Fatalf("Field2: %#v == %#v", &in.Field2, &out.Field2)
	}
	if &in.Field3 == &out.Field3 {
		t.Fatalf("Field3: %#v == %#v", &in.Field3, &out.Field3)
	}
	if &in.Field4 == &out.Field4 {
		t.Fatalf("Field4: %#v == %#v", &in.Field4, &out.Field4)
	}
	if &in.Field5 == &out.Field5 {
		t.Fatalf("Field5: %#v == %#v", &in.Field5, &out.Field5)
	}
	if &in.Field6 == &out.Field6 {
		t.Fatalf("Field6: %#v == %#v", &in.Field6, &out.Field6)
	}
	if &in.Field7 == &out.Field7 {
		t.Fatalf("Field7: %#v == %#v", &in.Field7, &out.Field7)
	}
	if &in.Field8 == &out.Field8 {
		t.Fatalf("Field8: %#v == %#v", &in.Field8, &out.Field8)
	}
	if &in.Field9 == &out.Field9 {
		t.Fatalf("Field9: %#v == %#v", &in.Field9, &out.Field9)
	}
	if &in.Field10 == &out.Field10 {
		t.Fatalf("Field10: %#v == %#v", &in.Field10, &out.Field10)
	}
	if &in.Field11 == &out.Field11 {
		t.Fatalf("Field11: %#v == %#v", &in.Field11, &out.Field11)
	}
	if &in.Field12 == &out.Field12 {
		t.Fatalf("Field12: %#v == %#v", &in.Field12, &out.Field12)
	}
	if &in.Field13 == &out.Field13 {
		t.Fatalf("Field13: %#v == %#v", &in.Field13, &out.Field13)
	}
	if &in.Field14 == &out.Field14 {
		t.Fatalf("Field14: %#v == %#v", &in.Field14, &out.Field14)
	}
	if &in.Field15 == &out.Field15 {
		t.Fatalf("Field15: %#v == %#v", &in.Field15, &out.Field15)
	}

	in = nil
	out = in.Copy()
	if out != nil {
		t.Fatalf("copying nil should return nil, returned: %#v", out)
	}
}

func TestRepeatedScalarPackedCopy(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	in := NewPopulatedRepeatedScalarPacked(popr, true)
	out := in.Copy()
	if !in.Equal(out) {
		t.Fatalf("%#v != %#v", in, out)
	}
	if &in.Field1 == &out.Field1 {
		t.Fatalf("Field1: %#v == %#v", &in.Field1, &out.Field1)
	}
	if &in.Field2 == &out.Field2 {
		t.Fatalf("Field2: %#v == %#v", &in.Field2, &out.Field2)
	}
	if &in.Field3 == &out.Field3 {
		t.Fatalf("Field3: %#v == %#v", &in.Field3, &out.Field3)
	}
	if &in.Field4 == &out.Field4 {
		t.Fatalf("Field4: %#v == %#v", &in.Field4, &out.Field4)
	}
	if &in.Field5 == &out.Field5 {
		t.Fatalf("Field5: %#v == %#v", &in.Field5, &out.Field5)
	}
	if &in.Field6 == &out.Field6 {
		t.Fatalf("Field6: %#v == %#v", &in.Field6, &out.Field6)
	}
	if &in.Field7 == &out.Field7 {
		t.Fatalf("Field7: %#v == %#v", &in.Field7, &out.Field7)
	}
	if &in.Field8 == &out.Field8 {
		t.Fatalf("Field8: %#v == %#v", &in.Field8, &out.Field8)
	}
	if &in.Field9 == &out.Field9 {
		t.Fatalf("Field9: %#v == %#v", &in.Field9, &out.Field9)
	}
	if &in.Field10 == &out.Field10 {
		t.Fatalf("Field10: %#v == %#v", &in.Field10, &out.Field10)
	}
	if &in.Field11 == &out.Field11 {
		t.Fatalf("Field11: %#v == %#v", &in.Field11, &out.Field11)
	}
	if &in.Field12 == &out.Field12 {
		t.Fatalf("Field12: %#v == %#v", &in.Field12, &out.Field12)
	}
	if &in.Field13 == &out.Field13 {
		t.Fatalf("Field13: %#v == %#v", &in.Field13, &out.Field13)
	}

	in = nil
	out = in.Copy()
	if out != nil {
		t.Fatalf("copying nil should return nil, returned: %#v", out)
	}
}

func TestExternalStructCopy(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	in := NewPopulatedExternalStruct(popr, true)
	out := in.Copy()
	if !in.Equal(out) {
		t.Fatalf("%#v != %#v", in, out)
	}
	if &in.Field1 == &out.Field1 {
		t.Fatalf("Field1: %#v == %#v", &in.Field1, &out.Field1)
	}
	if &in.Field2 == &out.Field2 {
		t.Fatalf("Field2: %#v == %#v", &in.Field2, &out.Field2)
	}
	if &in.Field3 == &out.Field3 {
		t.Fatalf("Field3: %#v == %#v", &in.Field3, &out.Field3)
	}

	in = nil
	out = in.Copy()
	if out != nil {
		t.Fatalf("copying nil should return nil, returned: %#v", out)
	}
}

func TestRepeatedExternalStructCopy(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	in := NewPopulatedRepeatedExternalStruct(popr, true)
	out := in.Copy()
	if !in.Equal(out) {
		t.Fatalf("%#v != %#v", in, out)
	}
	if &in.Field1 == &out.Field1 {
		t.Fatalf("Field1: %#v == %#v", &in.Field1, &out.Field1)
	}
	if &in.Field2 == &out.Field2 {
		t.Fatalf("Field2: %#v == %#v", &in.Field2, &out.Field2)
	}
	if &in.Field3 == &out.Field3 {
		t.Fatalf("Field3: %#v == %#v", &in.Field3, &out.Field3)
	}

	in = nil
	out = in.Copy()
	if out != nil {
		t.Fatalf("copying nil should return nil, returned: %#v", out)
	}
}

func TestNonNullableExternalStructCopy(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	in := NewPopulatedNonNullableExternalStruct(popr, true)
	out := in.Copy()
	if !in.Equal(out) {
		t.Fatalf("%#v != %#v", in, out)
	}
	if &in.Field1 == &out.Field1 {
		t.Fatalf("Field1: %#v == %#v", &in.Field1, &out.Field1)
	}
	if &in.Field2 == &out.Field2 {
		t.Fatalf("Field2: %#v == %#v", &in.Field2, &out.Field2)
	}
	if &in.Field3 == &out.Field3 {
		t.Fatalf("Field3: %#v == %#v", &in.Field3, &out.Field3)
	}

	in = nil
	out = in.Copy()
	if out != nil {
		t.Fatalf("copying nil should return nil, returned: %#v", out)
	}
}

func TestRepeatedNonNullableExternalStructCopy(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	in := NewPopulatedRepeatedNonNullableExternalStruct(popr, true)
	out := in.Copy()
	if !in.Equal(out) {
		t.Fatalf("%#v != %#v", in, out)
	}
	if &in.Field1 == &out.Field1 {
		t.Fatalf("Field1: %#v == %#v", &in.Field1, &out.Field1)
	}
	if &in.Field2 == &out.Field2 {
		t.Fatalf("Field2: %#v == %#v", &in.Field2, &out.Field2)
	}
	if &in.Field3 == &out.Field3 {
		t.Fatalf("Field3: %#v == %#v", &in.Field3, &out.Field3)
	}

	in = nil
	out = in.Copy()
	if out != nil {
		t.Fatalf("copying nil should return nil, returned: %#v", out)
	}
}

func TestMapStructCopy(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	in := NewPopulatedMapStruct(popr, true)
	out := in.Copy()
	if !in.Equal(out) {
		t.Fatalf("%#v != %#v", in, out)
	}
	if &in.NullableMap == &out.NullableMap {
		t.Fatalf("NullableMap: %#v == %#v", &in.NullableMap, &out.NullableMap)
	}
	if &in.NonnullableMap == &out.NonnullableMap {
		t.Fatalf("NonnullableMap: %#v == %#v", &in.NonnullableMap, &out.NonnullableMap)
	}

	in = nil
	out = in.Copy()
	if out != nil {
		t.Fatalf("copying nil should return nil, returned: %#v", out)
	}
}

func TestOneOfCopy(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	in := NewPopulatedOneOf(popr, true)
	out := in.Copy()
	if !in.Equal(out) {
		t.Fatalf("%#v != %#v", in, out)
	}
	if &in.Fields == &out.Fields {
		t.Fatalf("Fields: %#v == %#v", &in.Fields, &out.Fields)
	}
	if &in.Fields == &out.Fields {
		t.Fatalf("Fields: %#v == %#v", &in.Fields, &out.Fields)
	}
	if &in.Fields == &out.Fields {
		t.Fatalf("Fields: %#v == %#v", &in.Fields, &out.Fields)
	}
	if &in.Fields == &out.Fields {
		t.Fatalf("Fields: %#v == %#v", &in.Fields, &out.Fields)
	}
	if &in.Fields == &out.Fields {
		t.Fatalf("Fields: %#v == %#v", &in.Fields, &out.Fields)
	}
	if &in.Fields == &out.Fields {
		t.Fatalf("Fields: %#v == %#v", &in.Fields, &out.Fields)
	}
	if &in.Fields == &out.Fields {
		t.Fatalf("Fields: %#v == %#v", &in.Fields, &out.Fields)
	}
	if &in.Fields == &out.Fields {
		t.Fatalf("Fields: %#v == %#v", &in.Fields, &out.Fields)
	}
	if &in.Fields == &out.Fields {
		t.Fatalf("Fields: %#v == %#v", &in.Fields, &out.Fields)
	}
	if &in.FieldsTwo == &out.FieldsTwo {
		t.Fatalf("FieldsTwo: %#v == %#v", &in.FieldsTwo, &out.FieldsTwo)
	}
	if &in.FieldsTwo == &out.FieldsTwo {
		t.Fatalf("FieldsTwo: %#v == %#v", &in.FieldsTwo, &out.FieldsTwo)
	}

	in = nil
	out = in.Copy()
	if out != nil {
		t.Fatalf("copying nil should return nil, returned: %#v", out)
	}
}
func TestBasicScalarStringer(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	p := NewPopulatedBasicScalar(popr, false)
	s1 := p.String()
	s2 := fmt.Sprintf("%v", p)
	if s1 != s2 {
		t.Fatalf("String want %v got %v", s1, s2)
	}
}
func TestRepeatedScalarStringer(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	p := NewPopulatedRepeatedScalar(popr, false)
	s1 := p.String()
	s2 := fmt.Sprintf("%v", p)
	if s1 != s2 {
		t.Fatalf("String want %v got %v", s1, s2)
	}
}
func TestRepeatedScalarPackedStringer(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	p := NewPopulatedRepeatedScalarPacked(popr, false)
	s1 := p.String()
	s2 := fmt.Sprintf("%v", p)
	if s1 != s2 {
		t.Fatalf("String want %v got %v", s1, s2)
	}
}
func TestExternalStructStringer(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	p := NewPopulatedExternalStruct(popr, false)
	s1 := p.String()
	s2 := fmt.Sprintf("%v", p)
	if s1 != s2 {
		t.Fatalf("String want %v got %v", s1, s2)
	}
}
func TestRepeatedExternalStructStringer(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	p := NewPopulatedRepeatedExternalStruct(popr, false)
	s1 := p.String()
	s2 := fmt.Sprintf("%v", p)
	if s1 != s2 {
		t.Fatalf("String want %v got %v", s1, s2)
	}
}
func TestNonNullableExternalStructStringer(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	p := NewPopulatedNonNullableExternalStruct(popr, false)
	s1 := p.String()
	s2 := fmt.Sprintf("%v", p)
	if s1 != s2 {
		t.Fatalf("String want %v got %v", s1, s2)
	}
}
func TestRepeatedNonNullableExternalStructStringer(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	p := NewPopulatedRepeatedNonNullableExternalStruct(popr, false)
	s1 := p.String()
	s2 := fmt.Sprintf("%v", p)
	if s1 != s2 {
		t.Fatalf("String want %v got %v", s1, s2)
	}
}
func TestMapStructStringer(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	p := NewPopulatedMapStruct(popr, false)
	s1 := p.String()
	s2 := fmt.Sprintf("%v", p)
	if s1 != s2 {
		t.Fatalf("String want %v got %v", s1, s2)
	}
}
func TestOneOfStringer(t *testing.T) {
	popr := math_rand.New(math_rand.NewSource(time.Now().UnixNano()))
	p := NewPopulatedOneOf(popr, false)
	s1 := p.String()
	s2 := fmt.Sprintf("%v", p)
	if s1 != s2 {
		t.Fatalf("String want %v got %v", s1, s2)
	}
}

//These tests are generated by github.com/gogo/protobuf/plugin/testgen
