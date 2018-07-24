// Code generated by protoc-gen-gogo. DO NOT EDIT.
// source: github.com/docker/swarmkit/api/snapshot.proto

package api

import proto "github.com/gogo/protobuf/proto"
import fmt "fmt"
import math "math"

// skipping weak import gogoproto "github.com/gogo/protobuf/gogoproto"

import github_com_docker_swarmkit_api_deepcopy "github.com/docker/swarmkit/api/deepcopy"

import strings "strings"
import reflect "reflect"

import io "io"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.GoGoProtoPackageIsVersion2 // please upgrade the proto package

type Snapshot_Version int32

const (
	// V0 is the initial version of the StoreSnapshot message.
	Snapshot_V0 Snapshot_Version = 0
)

var Snapshot_Version_name = map[int32]string{
	0: "V0",
}
var Snapshot_Version_value = map[string]int32{
	"V0": 0,
}

func (x Snapshot_Version) String() string {
	return proto.EnumName(Snapshot_Version_name, int32(x))
}
func (Snapshot_Version) EnumDescriptor() ([]byte, []int) {
	return fileDescriptor_snapshot_a25ff9c091135b2f, []int{2, 0}
}

// StoreSnapshot is used to store snapshots of the store.
type StoreSnapshot struct {
	Nodes                []*Node      `protobuf:"bytes,1,rep,name=nodes" json:"nodes,omitempty"`
	Services             []*Service   `protobuf:"bytes,2,rep,name=services" json:"services,omitempty"`
	Networks             []*Network   `protobuf:"bytes,3,rep,name=networks" json:"networks,omitempty"`
	Tasks                []*Task      `protobuf:"bytes,4,rep,name=tasks" json:"tasks,omitempty"`
	Clusters             []*Cluster   `protobuf:"bytes,5,rep,name=clusters" json:"clusters,omitempty"`
	Secrets              []*Secret    `protobuf:"bytes,6,rep,name=secrets" json:"secrets,omitempty"`
	Resources            []*Resource  `protobuf:"bytes,7,rep,name=resources" json:"resources,omitempty"`
	Extensions           []*Extension `protobuf:"bytes,8,rep,name=extensions" json:"extensions,omitempty"`
	Configs              []*Config    `protobuf:"bytes,9,rep,name=configs" json:"configs,omitempty"`
	XXX_NoUnkeyedLiteral struct{}     `json:"-"`
	XXX_unrecognized     []byte       `json:"-"`
	XXX_sizecache        int32        `json:"-"`
}

func (m *StoreSnapshot) Reset()      { *m = StoreSnapshot{} }
func (*StoreSnapshot) ProtoMessage() {}
func (*StoreSnapshot) Descriptor() ([]byte, []int) {
	return fileDescriptor_snapshot_a25ff9c091135b2f, []int{0}
}
func (m *StoreSnapshot) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *StoreSnapshot) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_StoreSnapshot.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalTo(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (dst *StoreSnapshot) XXX_Merge(src proto.Message) {
	xxx_messageInfo_StoreSnapshot.Merge(dst, src)
}
func (m *StoreSnapshot) XXX_Size() int {
	return m.Size()
}
func (m *StoreSnapshot) XXX_DiscardUnknown() {
	xxx_messageInfo_StoreSnapshot.DiscardUnknown(m)
}

var xxx_messageInfo_StoreSnapshot proto.InternalMessageInfo

// ClusterSnapshot stores cluster membership information in snapshots.
type ClusterSnapshot struct {
	Members              []*RaftMember `protobuf:"bytes,1,rep,name=members" json:"members,omitempty"`
	Removed              []uint64      `protobuf:"varint,2,rep,name=removed" json:"removed,omitempty"`
	XXX_NoUnkeyedLiteral struct{}      `json:"-"`
	XXX_unrecognized     []byte        `json:"-"`
	XXX_sizecache        int32         `json:"-"`
}

func (m *ClusterSnapshot) Reset()      { *m = ClusterSnapshot{} }
func (*ClusterSnapshot) ProtoMessage() {}
func (*ClusterSnapshot) Descriptor() ([]byte, []int) {
	return fileDescriptor_snapshot_a25ff9c091135b2f, []int{1}
}
func (m *ClusterSnapshot) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *ClusterSnapshot) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_ClusterSnapshot.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalTo(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (dst *ClusterSnapshot) XXX_Merge(src proto.Message) {
	xxx_messageInfo_ClusterSnapshot.Merge(dst, src)
}
func (m *ClusterSnapshot) XXX_Size() int {
	return m.Size()
}
func (m *ClusterSnapshot) XXX_DiscardUnknown() {
	xxx_messageInfo_ClusterSnapshot.DiscardUnknown(m)
}

var xxx_messageInfo_ClusterSnapshot proto.InternalMessageInfo

type Snapshot struct {
	Version              Snapshot_Version `protobuf:"varint,1,opt,name=version,proto3,enum=docker.swarmkit.v1.Snapshot_Version" json:"version,omitempty"`
	Membership           ClusterSnapshot  `protobuf:"bytes,2,opt,name=membership" json:"membership"`
	Store                StoreSnapshot    `protobuf:"bytes,3,opt,name=store" json:"store"`
	XXX_NoUnkeyedLiteral struct{}         `json:"-"`
	XXX_unrecognized     []byte           `json:"-"`
	XXX_sizecache        int32            `json:"-"`
}

func (m *Snapshot) Reset()      { *m = Snapshot{} }
func (*Snapshot) ProtoMessage() {}
func (*Snapshot) Descriptor() ([]byte, []int) {
	return fileDescriptor_snapshot_a25ff9c091135b2f, []int{2}
}
func (m *Snapshot) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *Snapshot) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_Snapshot.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalTo(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (dst *Snapshot) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Snapshot.Merge(dst, src)
}
func (m *Snapshot) XXX_Size() int {
	return m.Size()
}
func (m *Snapshot) XXX_DiscardUnknown() {
	xxx_messageInfo_Snapshot.DiscardUnknown(m)
}

var xxx_messageInfo_Snapshot proto.InternalMessageInfo

func init() {
	proto.RegisterType((*StoreSnapshot)(nil), "docker.swarmkit.v1.StoreSnapshot")
	proto.RegisterType((*ClusterSnapshot)(nil), "docker.swarmkit.v1.ClusterSnapshot")
	proto.RegisterType((*Snapshot)(nil), "docker.swarmkit.v1.Snapshot")
	proto.RegisterEnum("docker.swarmkit.v1.Snapshot_Version", Snapshot_Version_name, Snapshot_Version_value)
}

func (m *StoreSnapshot) Copy() *StoreSnapshot {
	if m == nil {
		return nil
	}
	o := &StoreSnapshot{}
	o.CopyFrom(m)
	return o
}

func (m *StoreSnapshot) CopyFrom(src interface{}) {

	o := src.(*StoreSnapshot)
	*m = *o
	if o.Nodes != nil {
		m.Nodes = make([]*Node, len(o.Nodes))
		for i := range m.Nodes {
			m.Nodes[i] = &Node{}
			github_com_docker_swarmkit_api_deepcopy.Copy(m.Nodes[i], o.Nodes[i])
		}
	}

	if o.Services != nil {
		m.Services = make([]*Service, len(o.Services))
		for i := range m.Services {
			m.Services[i] = &Service{}
			github_com_docker_swarmkit_api_deepcopy.Copy(m.Services[i], o.Services[i])
		}
	}

	if o.Networks != nil {
		m.Networks = make([]*Network, len(o.Networks))
		for i := range m.Networks {
			m.Networks[i] = &Network{}
			github_com_docker_swarmkit_api_deepcopy.Copy(m.Networks[i], o.Networks[i])
		}
	}

	if o.Tasks != nil {
		m.Tasks = make([]*Task, len(o.Tasks))
		for i := range m.Tasks {
			m.Tasks[i] = &Task{}
			github_com_docker_swarmkit_api_deepcopy.Copy(m.Tasks[i], o.Tasks[i])
		}
	}

	if o.Clusters != nil {
		m.Clusters = make([]*Cluster, len(o.Clusters))
		for i := range m.Clusters {
			m.Clusters[i] = &Cluster{}
			github_com_docker_swarmkit_api_deepcopy.Copy(m.Clusters[i], o.Clusters[i])
		}
	}

	if o.Secrets != nil {
		m.Secrets = make([]*Secret, len(o.Secrets))
		for i := range m.Secrets {
			m.Secrets[i] = &Secret{}
			github_com_docker_swarmkit_api_deepcopy.Copy(m.Secrets[i], o.Secrets[i])
		}
	}

	if o.Resources != nil {
		m.Resources = make([]*Resource, len(o.Resources))
		for i := range m.Resources {
			m.Resources[i] = &Resource{}
			github_com_docker_swarmkit_api_deepcopy.Copy(m.Resources[i], o.Resources[i])
		}
	}

	if o.Extensions != nil {
		m.Extensions = make([]*Extension, len(o.Extensions))
		for i := range m.Extensions {
			m.Extensions[i] = &Extension{}
			github_com_docker_swarmkit_api_deepcopy.Copy(m.Extensions[i], o.Extensions[i])
		}
	}

	if o.Configs != nil {
		m.Configs = make([]*Config, len(o.Configs))
		for i := range m.Configs {
			m.Configs[i] = &Config{}
			github_com_docker_swarmkit_api_deepcopy.Copy(m.Configs[i], o.Configs[i])
		}
	}

}

func (m *ClusterSnapshot) Copy() *ClusterSnapshot {
	if m == nil {
		return nil
	}
	o := &ClusterSnapshot{}
	o.CopyFrom(m)
	return o
}

func (m *ClusterSnapshot) CopyFrom(src interface{}) {

	o := src.(*ClusterSnapshot)
	*m = *o
	if o.Members != nil {
		m.Members = make([]*RaftMember, len(o.Members))
		for i := range m.Members {
			m.Members[i] = &RaftMember{}
			github_com_docker_swarmkit_api_deepcopy.Copy(m.Members[i], o.Members[i])
		}
	}

	if o.Removed != nil {
		m.Removed = make([]uint64, len(o.Removed))
		copy(m.Removed, o.Removed)
	}

}

func (m *Snapshot) Copy() *Snapshot {
	if m == nil {
		return nil
	}
	o := &Snapshot{}
	o.CopyFrom(m)
	return o
}

func (m *Snapshot) CopyFrom(src interface{}) {

	o := src.(*Snapshot)
	*m = *o
	github_com_docker_swarmkit_api_deepcopy.Copy(&m.Membership, &o.Membership)
	github_com_docker_swarmkit_api_deepcopy.Copy(&m.Store, &o.Store)
}

func (m *StoreSnapshot) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalTo(dAtA)
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *StoreSnapshot) MarshalTo(dAtA []byte) (int, error) {
	var i int
	_ = i
	var l int
	_ = l
	if len(m.Nodes) > 0 {
		for _, msg := range m.Nodes {
			dAtA[i] = 0xa
			i++
			i = encodeVarintSnapshot(dAtA, i, uint64(msg.Size()))
			n, err := msg.MarshalTo(dAtA[i:])
			if err != nil {
				return 0, err
			}
			i += n
		}
	}
	if len(m.Services) > 0 {
		for _, msg := range m.Services {
			dAtA[i] = 0x12
			i++
			i = encodeVarintSnapshot(dAtA, i, uint64(msg.Size()))
			n, err := msg.MarshalTo(dAtA[i:])
			if err != nil {
				return 0, err
			}
			i += n
		}
	}
	if len(m.Networks) > 0 {
		for _, msg := range m.Networks {
			dAtA[i] = 0x1a
			i++
			i = encodeVarintSnapshot(dAtA, i, uint64(msg.Size()))
			n, err := msg.MarshalTo(dAtA[i:])
			if err != nil {
				return 0, err
			}
			i += n
		}
	}
	if len(m.Tasks) > 0 {
		for _, msg := range m.Tasks {
			dAtA[i] = 0x22
			i++
			i = encodeVarintSnapshot(dAtA, i, uint64(msg.Size()))
			n, err := msg.MarshalTo(dAtA[i:])
			if err != nil {
				return 0, err
			}
			i += n
		}
	}
	if len(m.Clusters) > 0 {
		for _, msg := range m.Clusters {
			dAtA[i] = 0x2a
			i++
			i = encodeVarintSnapshot(dAtA, i, uint64(msg.Size()))
			n, err := msg.MarshalTo(dAtA[i:])
			if err != nil {
				return 0, err
			}
			i += n
		}
	}
	if len(m.Secrets) > 0 {
		for _, msg := range m.Secrets {
			dAtA[i] = 0x32
			i++
			i = encodeVarintSnapshot(dAtA, i, uint64(msg.Size()))
			n, err := msg.MarshalTo(dAtA[i:])
			if err != nil {
				return 0, err
			}
			i += n
		}
	}
	if len(m.Resources) > 0 {
		for _, msg := range m.Resources {
			dAtA[i] = 0x3a
			i++
			i = encodeVarintSnapshot(dAtA, i, uint64(msg.Size()))
			n, err := msg.MarshalTo(dAtA[i:])
			if err != nil {
				return 0, err
			}
			i += n
		}
	}
	if len(m.Extensions) > 0 {
		for _, msg := range m.Extensions {
			dAtA[i] = 0x42
			i++
			i = encodeVarintSnapshot(dAtA, i, uint64(msg.Size()))
			n, err := msg.MarshalTo(dAtA[i:])
			if err != nil {
				return 0, err
			}
			i += n
		}
	}
	if len(m.Configs) > 0 {
		for _, msg := range m.Configs {
			dAtA[i] = 0x4a
			i++
			i = encodeVarintSnapshot(dAtA, i, uint64(msg.Size()))
			n, err := msg.MarshalTo(dAtA[i:])
			if err != nil {
				return 0, err
			}
			i += n
		}
	}
	if m.XXX_unrecognized != nil {
		i += copy(dAtA[i:], m.XXX_unrecognized)
	}
	return i, nil
}

func (m *ClusterSnapshot) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalTo(dAtA)
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *ClusterSnapshot) MarshalTo(dAtA []byte) (int, error) {
	var i int
	_ = i
	var l int
	_ = l
	if len(m.Members) > 0 {
		for _, msg := range m.Members {
			dAtA[i] = 0xa
			i++
			i = encodeVarintSnapshot(dAtA, i, uint64(msg.Size()))
			n, err := msg.MarshalTo(dAtA[i:])
			if err != nil {
				return 0, err
			}
			i += n
		}
	}
	if len(m.Removed) > 0 {
		for _, num := range m.Removed {
			dAtA[i] = 0x10
			i++
			i = encodeVarintSnapshot(dAtA, i, uint64(num))
		}
	}
	if m.XXX_unrecognized != nil {
		i += copy(dAtA[i:], m.XXX_unrecognized)
	}
	return i, nil
}

func (m *Snapshot) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalTo(dAtA)
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *Snapshot) MarshalTo(dAtA []byte) (int, error) {
	var i int
	_ = i
	var l int
	_ = l
	if m.Version != 0 {
		dAtA[i] = 0x8
		i++
		i = encodeVarintSnapshot(dAtA, i, uint64(m.Version))
	}
	dAtA[i] = 0x12
	i++
	i = encodeVarintSnapshot(dAtA, i, uint64(m.Membership.Size()))
	n1, err := m.Membership.MarshalTo(dAtA[i:])
	if err != nil {
		return 0, err
	}
	i += n1
	dAtA[i] = 0x1a
	i++
	i = encodeVarintSnapshot(dAtA, i, uint64(m.Store.Size()))
	n2, err := m.Store.MarshalTo(dAtA[i:])
	if err != nil {
		return 0, err
	}
	i += n2
	if m.XXX_unrecognized != nil {
		i += copy(dAtA[i:], m.XXX_unrecognized)
	}
	return i, nil
}

func encodeVarintSnapshot(dAtA []byte, offset int, v uint64) int {
	for v >= 1<<7 {
		dAtA[offset] = uint8(v&0x7f | 0x80)
		v >>= 7
		offset++
	}
	dAtA[offset] = uint8(v)
	return offset + 1
}

func (m *StoreSnapshot) Size() (n int) {
	var l int
	_ = l
	if len(m.Nodes) > 0 {
		for _, e := range m.Nodes {
			l = e.Size()
			n += 1 + l + sovSnapshot(uint64(l))
		}
	}
	if len(m.Services) > 0 {
		for _, e := range m.Services {
			l = e.Size()
			n += 1 + l + sovSnapshot(uint64(l))
		}
	}
	if len(m.Networks) > 0 {
		for _, e := range m.Networks {
			l = e.Size()
			n += 1 + l + sovSnapshot(uint64(l))
		}
	}
	if len(m.Tasks) > 0 {
		for _, e := range m.Tasks {
			l = e.Size()
			n += 1 + l + sovSnapshot(uint64(l))
		}
	}
	if len(m.Clusters) > 0 {
		for _, e := range m.Clusters {
			l = e.Size()
			n += 1 + l + sovSnapshot(uint64(l))
		}
	}
	if len(m.Secrets) > 0 {
		for _, e := range m.Secrets {
			l = e.Size()
			n += 1 + l + sovSnapshot(uint64(l))
		}
	}
	if len(m.Resources) > 0 {
		for _, e := range m.Resources {
			l = e.Size()
			n += 1 + l + sovSnapshot(uint64(l))
		}
	}
	if len(m.Extensions) > 0 {
		for _, e := range m.Extensions {
			l = e.Size()
			n += 1 + l + sovSnapshot(uint64(l))
		}
	}
	if len(m.Configs) > 0 {
		for _, e := range m.Configs {
			l = e.Size()
			n += 1 + l + sovSnapshot(uint64(l))
		}
	}
	if m.XXX_unrecognized != nil {
		n += len(m.XXX_unrecognized)
	}
	return n
}

func (m *ClusterSnapshot) Size() (n int) {
	var l int
	_ = l
	if len(m.Members) > 0 {
		for _, e := range m.Members {
			l = e.Size()
			n += 1 + l + sovSnapshot(uint64(l))
		}
	}
	if len(m.Removed) > 0 {
		for _, e := range m.Removed {
			n += 1 + sovSnapshot(uint64(e))
		}
	}
	if m.XXX_unrecognized != nil {
		n += len(m.XXX_unrecognized)
	}
	return n
}

func (m *Snapshot) Size() (n int) {
	var l int
	_ = l
	if m.Version != 0 {
		n += 1 + sovSnapshot(uint64(m.Version))
	}
	l = m.Membership.Size()
	n += 1 + l + sovSnapshot(uint64(l))
	l = m.Store.Size()
	n += 1 + l + sovSnapshot(uint64(l))
	if m.XXX_unrecognized != nil {
		n += len(m.XXX_unrecognized)
	}
	return n
}

func sovSnapshot(x uint64) (n int) {
	for {
		n++
		x >>= 7
		if x == 0 {
			break
		}
	}
	return n
}
func sozSnapshot(x uint64) (n int) {
	return sovSnapshot(uint64((x << 1) ^ uint64((int64(x) >> 63))))
}
func (this *StoreSnapshot) String() string {
	if this == nil {
		return "nil"
	}
	s := strings.Join([]string{`&StoreSnapshot{`,
		`Nodes:` + strings.Replace(fmt.Sprintf("%v", this.Nodes), "Node", "Node", 1) + `,`,
		`Services:` + strings.Replace(fmt.Sprintf("%v", this.Services), "Service", "Service", 1) + `,`,
		`Networks:` + strings.Replace(fmt.Sprintf("%v", this.Networks), "Network", "Network", 1) + `,`,
		`Tasks:` + strings.Replace(fmt.Sprintf("%v", this.Tasks), "Task", "Task", 1) + `,`,
		`Clusters:` + strings.Replace(fmt.Sprintf("%v", this.Clusters), "Cluster", "Cluster", 1) + `,`,
		`Secrets:` + strings.Replace(fmt.Sprintf("%v", this.Secrets), "Secret", "Secret", 1) + `,`,
		`Resources:` + strings.Replace(fmt.Sprintf("%v", this.Resources), "Resource", "Resource", 1) + `,`,
		`Extensions:` + strings.Replace(fmt.Sprintf("%v", this.Extensions), "Extension", "Extension", 1) + `,`,
		`Configs:` + strings.Replace(fmt.Sprintf("%v", this.Configs), "Config", "Config", 1) + `,`,
		`XXX_unrecognized:` + fmt.Sprintf("%v", this.XXX_unrecognized) + `,`,
		`}`,
	}, "")
	return s
}
func (this *ClusterSnapshot) String() string {
	if this == nil {
		return "nil"
	}
	s := strings.Join([]string{`&ClusterSnapshot{`,
		`Members:` + strings.Replace(fmt.Sprintf("%v", this.Members), "RaftMember", "RaftMember", 1) + `,`,
		`Removed:` + fmt.Sprintf("%v", this.Removed) + `,`,
		`XXX_unrecognized:` + fmt.Sprintf("%v", this.XXX_unrecognized) + `,`,
		`}`,
	}, "")
	return s
}
func (this *Snapshot) String() string {
	if this == nil {
		return "nil"
	}
	s := strings.Join([]string{`&Snapshot{`,
		`Version:` + fmt.Sprintf("%v", this.Version) + `,`,
		`Membership:` + strings.Replace(strings.Replace(this.Membership.String(), "ClusterSnapshot", "ClusterSnapshot", 1), `&`, ``, 1) + `,`,
		`Store:` + strings.Replace(strings.Replace(this.Store.String(), "StoreSnapshot", "StoreSnapshot", 1), `&`, ``, 1) + `,`,
		`XXX_unrecognized:` + fmt.Sprintf("%v", this.XXX_unrecognized) + `,`,
		`}`,
	}, "")
	return s
}
func valueToStringSnapshot(v interface{}) string {
	rv := reflect.ValueOf(v)
	if rv.IsNil() {
		return "nil"
	}
	pv := reflect.Indirect(rv).Interface()
	return fmt.Sprintf("*%v", pv)
}
func (m *StoreSnapshot) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowSnapshot
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= (uint64(b) & 0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: StoreSnapshot: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: StoreSnapshot: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Nodes", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowSnapshot
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthSnapshot
			}
			postIndex := iNdEx + msglen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Nodes = append(m.Nodes, &Node{})
			if err := m.Nodes[len(m.Nodes)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 2:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Services", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowSnapshot
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthSnapshot
			}
			postIndex := iNdEx + msglen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Services = append(m.Services, &Service{})
			if err := m.Services[len(m.Services)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 3:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Networks", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowSnapshot
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthSnapshot
			}
			postIndex := iNdEx + msglen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Networks = append(m.Networks, &Network{})
			if err := m.Networks[len(m.Networks)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 4:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Tasks", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowSnapshot
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthSnapshot
			}
			postIndex := iNdEx + msglen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Tasks = append(m.Tasks, &Task{})
			if err := m.Tasks[len(m.Tasks)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 5:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Clusters", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowSnapshot
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthSnapshot
			}
			postIndex := iNdEx + msglen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Clusters = append(m.Clusters, &Cluster{})
			if err := m.Clusters[len(m.Clusters)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 6:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Secrets", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowSnapshot
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthSnapshot
			}
			postIndex := iNdEx + msglen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Secrets = append(m.Secrets, &Secret{})
			if err := m.Secrets[len(m.Secrets)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 7:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Resources", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowSnapshot
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthSnapshot
			}
			postIndex := iNdEx + msglen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Resources = append(m.Resources, &Resource{})
			if err := m.Resources[len(m.Resources)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 8:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Extensions", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowSnapshot
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthSnapshot
			}
			postIndex := iNdEx + msglen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Extensions = append(m.Extensions, &Extension{})
			if err := m.Extensions[len(m.Extensions)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 9:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Configs", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowSnapshot
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthSnapshot
			}
			postIndex := iNdEx + msglen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Configs = append(m.Configs, &Config{})
			if err := m.Configs[len(m.Configs)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		default:
			iNdEx = preIndex
			skippy, err := skipSnapshot(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if skippy < 0 {
				return ErrInvalidLengthSnapshot
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			m.XXX_unrecognized = append(m.XXX_unrecognized, dAtA[iNdEx:iNdEx+skippy]...)
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func (m *ClusterSnapshot) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowSnapshot
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= (uint64(b) & 0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: ClusterSnapshot: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: ClusterSnapshot: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Members", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowSnapshot
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthSnapshot
			}
			postIndex := iNdEx + msglen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Members = append(m.Members, &RaftMember{})
			if err := m.Members[len(m.Members)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 2:
			if wireType == 0 {
				var v uint64
				for shift := uint(0); ; shift += 7 {
					if shift >= 64 {
						return ErrIntOverflowSnapshot
					}
					if iNdEx >= l {
						return io.ErrUnexpectedEOF
					}
					b := dAtA[iNdEx]
					iNdEx++
					v |= (uint64(b) & 0x7F) << shift
					if b < 0x80 {
						break
					}
				}
				m.Removed = append(m.Removed, v)
			} else if wireType == 2 {
				var packedLen int
				for shift := uint(0); ; shift += 7 {
					if shift >= 64 {
						return ErrIntOverflowSnapshot
					}
					if iNdEx >= l {
						return io.ErrUnexpectedEOF
					}
					b := dAtA[iNdEx]
					iNdEx++
					packedLen |= (int(b) & 0x7F) << shift
					if b < 0x80 {
						break
					}
				}
				if packedLen < 0 {
					return ErrInvalidLengthSnapshot
				}
				postIndex := iNdEx + packedLen
				if postIndex > l {
					return io.ErrUnexpectedEOF
				}
				for iNdEx < postIndex {
					var v uint64
					for shift := uint(0); ; shift += 7 {
						if shift >= 64 {
							return ErrIntOverflowSnapshot
						}
						if iNdEx >= l {
							return io.ErrUnexpectedEOF
						}
						b := dAtA[iNdEx]
						iNdEx++
						v |= (uint64(b) & 0x7F) << shift
						if b < 0x80 {
							break
						}
					}
					m.Removed = append(m.Removed, v)
				}
			} else {
				return fmt.Errorf("proto: wrong wireType = %d for field Removed", wireType)
			}
		default:
			iNdEx = preIndex
			skippy, err := skipSnapshot(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if skippy < 0 {
				return ErrInvalidLengthSnapshot
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			m.XXX_unrecognized = append(m.XXX_unrecognized, dAtA[iNdEx:iNdEx+skippy]...)
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func (m *Snapshot) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowSnapshot
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= (uint64(b) & 0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: Snapshot: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: Snapshot: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field Version", wireType)
			}
			m.Version = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowSnapshot
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.Version |= (Snapshot_Version(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		case 2:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Membership", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowSnapshot
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthSnapshot
			}
			postIndex := iNdEx + msglen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			if err := m.Membership.Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 3:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Store", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowSnapshot
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthSnapshot
			}
			postIndex := iNdEx + msglen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			if err := m.Store.Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		default:
			iNdEx = preIndex
			skippy, err := skipSnapshot(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if skippy < 0 {
				return ErrInvalidLengthSnapshot
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			m.XXX_unrecognized = append(m.XXX_unrecognized, dAtA[iNdEx:iNdEx+skippy]...)
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func skipSnapshot(dAtA []byte) (n int, err error) {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return 0, ErrIntOverflowSnapshot
			}
			if iNdEx >= l {
				return 0, io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= (uint64(b) & 0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		wireType := int(wire & 0x7)
		switch wireType {
		case 0:
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return 0, ErrIntOverflowSnapshot
				}
				if iNdEx >= l {
					return 0, io.ErrUnexpectedEOF
				}
				iNdEx++
				if dAtA[iNdEx-1] < 0x80 {
					break
				}
			}
			return iNdEx, nil
		case 1:
			iNdEx += 8
			return iNdEx, nil
		case 2:
			var length int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return 0, ErrIntOverflowSnapshot
				}
				if iNdEx >= l {
					return 0, io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				length |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			iNdEx += length
			if length < 0 {
				return 0, ErrInvalidLengthSnapshot
			}
			return iNdEx, nil
		case 3:
			for {
				var innerWire uint64
				var start int = iNdEx
				for shift := uint(0); ; shift += 7 {
					if shift >= 64 {
						return 0, ErrIntOverflowSnapshot
					}
					if iNdEx >= l {
						return 0, io.ErrUnexpectedEOF
					}
					b := dAtA[iNdEx]
					iNdEx++
					innerWire |= (uint64(b) & 0x7F) << shift
					if b < 0x80 {
						break
					}
				}
				innerWireType := int(innerWire & 0x7)
				if innerWireType == 4 {
					break
				}
				next, err := skipSnapshot(dAtA[start:])
				if err != nil {
					return 0, err
				}
				iNdEx = start + next
			}
			return iNdEx, nil
		case 4:
			return iNdEx, nil
		case 5:
			iNdEx += 4
			return iNdEx, nil
		default:
			return 0, fmt.Errorf("proto: illegal wireType %d", wireType)
		}
	}
	panic("unreachable")
}

var (
	ErrInvalidLengthSnapshot = fmt.Errorf("proto: negative length found during unmarshaling")
	ErrIntOverflowSnapshot   = fmt.Errorf("proto: integer overflow")
)

func init() {
	proto.RegisterFile("github.com/docker/swarmkit/api/snapshot.proto", fileDescriptor_snapshot_a25ff9c091135b2f)
}

var fileDescriptor_snapshot_a25ff9c091135b2f = []byte{
	// 493 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x84, 0x93, 0xbf, 0x8f, 0xd3, 0x30,
	0x14, 0xc7, 0x93, 0xf4, 0x47, 0x7a, 0x3e, 0x01, 0x87, 0xc5, 0x60, 0x95, 0x23, 0x94, 0xc2, 0x50,
	0x24, 0x48, 0xa0, 0x20, 0x81, 0x90, 0x8e, 0xa1, 0x27, 0x06, 0x06, 0x6e, 0x70, 0xd1, 0x89, 0x35,
	0x4d, 0xdd, 0x36, 0x84, 0xc4, 0x95, 0x9f, 0xdb, 0x63, 0x84, 0xff, 0xae, 0x23, 0x23, 0x13, 0xe2,
	0xba, 0xf0, 0x6f, 0x20, 0xdb, 0x71, 0xa8, 0x44, 0x7a, 0xb7, 0x45, 0xd6, 0xe7, 0xf3, 0xde, 0xd7,
	0xce, 0x7b, 0xe8, 0xe9, 0x3c, 0x95, 0x8b, 0xd5, 0x24, 0x4c, 0x78, 0x1e, 0x4d, 0x79, 0x92, 0x31,
	0x11, 0xc1, 0x45, 0x2c, 0xf2, 0x2c, 0x95, 0x51, 0xbc, 0x4c, 0x23, 0x28, 0xe2, 0x25, 0x2c, 0xb8,
	0x0c, 0x97, 0x82, 0x4b, 0x8e, 0xb1, 0x61, 0x42, 0xcb, 0x84, 0xeb, 0xe7, 0xdd, 0x27, 0xd7, 0x94,
	0xe0, 0x93, 0xcf, 0x2c, 0x91, 0x60, 0x2a, 0x74, 0x1f, 0x5f, 0x43, 0x8b, 0x78, 0x56, 0x36, 0xeb,
	0xde, 0x99, 0xf3, 0x39, 0xd7, 0x9f, 0x91, 0xfa, 0x32, 0xa7, 0xfd, 0xef, 0x4d, 0x74, 0x63, 0x2c,
	0xb9, 0x60, 0xe3, 0x32, 0x1a, 0x0e, 0x51, 0xab, 0xe0, 0x53, 0x06, 0xc4, 0xed, 0x35, 0x06, 0x87,
	0x43, 0x12, 0xfe, 0x1f, 0x32, 0x3c, 0xe3, 0x53, 0x46, 0x0d, 0x86, 0x5f, 0xa1, 0x0e, 0x30, 0xb1,
	0x4e, 0x13, 0x06, 0xc4, 0xd3, 0xca, 0xdd, 0x3a, 0x65, 0x6c, 0x18, 0x5a, 0xc1, 0x4a, 0x2c, 0x98,
	0xbc, 0xe0, 0x22, 0x03, 0xd2, 0xd8, 0x2f, 0x9e, 0x19, 0x86, 0x56, 0xb0, 0x4a, 0x28, 0x63, 0xc8,
	0x80, 0x34, 0xf7, 0x27, 0xfc, 0x18, 0x43, 0x46, 0x0d, 0xa6, 0x1a, 0x25, 0x5f, 0x56, 0x20, 0x99,
	0x00, 0xd2, 0xda, 0xdf, 0xe8, 0xd4, 0x30, 0xb4, 0x82, 0xf1, 0x4b, 0xe4, 0x03, 0x4b, 0x04, 0x93,
	0x40, 0xda, 0xda, 0xeb, 0xd6, 0xdf, 0x4c, 0x21, 0xd4, 0xa2, 0xf8, 0x0d, 0x3a, 0x10, 0x0c, 0xf8,
	0x4a, 0xa8, 0x17, 0xf1, 0xb5, 0x77, 0x5c, 0xe7, 0xd1, 0x12, 0xa2, 0xff, 0x70, 0x7c, 0x82, 0x10,
	0xfb, 0x2a, 0x59, 0x01, 0x29, 0x2f, 0x80, 0x74, 0xb4, 0x7c, 0xaf, 0x4e, 0x7e, 0x67, 0x29, 0xba,
	0x23, 0xa8, 0xc0, 0x09, 0x2f, 0x66, 0xe9, 0x1c, 0xc8, 0xc1, 0xfe, 0xc0, 0xa7, 0x1a, 0xa1, 0x16,
	0xed, 0xa7, 0xe8, 0x56, 0x79, 0xf7, 0x6a, 0x08, 0x5e, 0x23, 0x3f, 0x67, 0xf9, 0x44, 0xbd, 0x98,
	0x19, 0x83, 0xa0, 0xf6, 0x06, 0xf1, 0x4c, 0x7e, 0xd0, 0x18, 0xb5, 0x38, 0x3e, 0x46, 0xbe, 0x60,
	0x39, 0x5f, 0xb3, 0xa9, 0x9e, 0x86, 0xe6, 0xc8, 0x3b, 0x72, 0xa8, 0x3d, 0xea, 0xff, 0x71, 0x51,
	0xa7, 0x6a, 0xf2, 0x16, 0xf9, 0x6b, 0x26, 0x54, 0x72, 0xe2, 0xf6, 0xdc, 0xc1, 0xcd, 0xe1, 0xa3,
	0xda, 0xe7, 0xb5, 0x3b, 0x73, 0x6e, 0x58, 0x6a, 0x25, 0xfc, 0x1e, 0xa1, 0xb2, 0xeb, 0x22, 0x5d,
	0x12, 0xaf, 0xe7, 0x0e, 0x0e, 0x87, 0x0f, 0xaf, 0xf8, 0xb3, 0xb6, 0xd2, 0xa8, 0xb9, 0xf9, 0x75,
	0xdf, 0xa1, 0x3b, 0x32, 0x3e, 0x41, 0x2d, 0x50, 0x5b, 0x40, 0x1a, 0xba, 0xca, 0x83, 0xda, 0x20,
	0xbb, 0x6b, 0x52, 0xd6, 0x30, 0x56, 0xff, 0x36, 0xf2, 0xcb, 0x74, 0xb8, 0x8d, 0xbc, 0xf3, 0x67,
	0x47, 0xce, 0x88, 0x6c, 0x2e, 0x03, 0xe7, 0xe7, 0x65, 0xe0, 0x7c, 0xdb, 0x06, 0xee, 0x66, 0x1b,
	0xb8, 0x3f, 0xb6, 0x81, 0xfb, 0x7b, 0x1b, 0xb8, 0x9f, 0xbc, 0x49, 0x5b, 0xef, 0xde, 0x8b, 0xbf,
	0x01, 0x00, 0x00, 0xff, 0xff, 0xfd, 0xbe, 0x47, 0xec, 0x2f, 0x04, 0x00, 0x00,
}
