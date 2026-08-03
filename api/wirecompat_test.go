package api

import (
	"bytes"
	"encoding/hex"
	"testing"

	"google.golang.org/protobuf/proto"
)

// The payloads below were produced by the gogo/protobuf-generated marshaller
// that this package used before the migration to google.golang.org/protobuf.
//
// They exist to guard the compatibility properties that migration had to
// preserve, because getting any of them wrong silently breaks rolling upgrades
// and makes existing raft logs and snapshots unreadable:
//
//   - embedded messages that used to be (gogoproto.nullable) = false and are
//     now plain pointers (Node.Meta, Node.Spec, Meta.Version, Task.Status, ...)
//   - repeated non-nullable messages, now slices of pointers
//     (Annotations.Indices, ContainerSpec.Mounts)
//   - map fields, whose key order the marshaller has to keep sorted
//   - (gogoproto.stdduration) fields, which were time.Duration and are now
//     *durationpb.Duration (UpdateConfig.Delay)
//   - (gogoproto.customtype) = "os.FileMode", which is now a plain uint32
//     (Mount.TmpfsOptions.Mode)
//
// A failure here means the encoding changed, not that the fixture is stale. Do
// not regenerate these constants to make the test pass.
const (
	gogoNodeHex = "0a066e6f64652d31121e0a02082a120b0880e2cfaa0610959aef3a1a0b0880e2cfaa0610959aef3a" +
		"1a350a2f0a096e6f64652d6e616d6512060a016112013112060a016212013222080a026b311202763122080a" +
		"026b3212027632100118012a130802120572656164791a0831302e302e302e3142131209637372" +
		"2d62797465731a0208032a02636e4801520d120b31302e302e302e322f323458b525"

	gogoTaskHex = "0a067461736b2d3112020a001a27220808021202080518030a1b0a03696d674214080212037372631a" +
		"037467743a0608800810ed0322057376632d31280732066e6f64652d313a0b0a097461736b2d6e616d654200" +
		"4a0c1080041a0772756e6e696e67508004"

	gogoServiceHex = "0a057376632d3112110a020807120b0880e2cfaa0610959aef3a1a150a050a037376631200320a08" +
		"02120208032202080952020807"
)

func TestGogoWireCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hex   string
		msg   proto.Message
		check func(*testing.T, proto.Message)
	}{
		{
			name: "Node",
			hex:  gogoNodeHex,
			msg:  &Node{},
			check: func(t *testing.T, m proto.Message) {
				n := m.(*Node)
				if n.GetId() != "node-1" {
					t.Errorf("Id = %q, want %q", n.GetId(), "node-1")
				}
				// Was Meta.Version, a non-nullable embedded message.
				if got := n.GetMeta().GetVersion().GetIndex(); got != 42 {
					t.Errorf("Meta.Version.Index = %d, want 42", got)
				}
				if got := n.GetSpec().GetAnnotations().GetName(); got != "node-name" {
					t.Errorf("Spec.Annotations.Name = %q, want %q", got, "node-name")
				}
				// Was []IndexEntry, a repeated non-nullable message.
				if got := len(n.GetSpec().GetAnnotations().GetIndices()); got != 2 {
					t.Errorf("len(Spec.GetAnnotations().GetIndices()) = %d, want 2", got)
				}
				if got := len(n.GetSpec().GetAnnotations().GetLabels()); got != 2 {
					t.Errorf("len(Spec.GetAnnotations().GetLabels()) = %d, want 2", got)
				}
				if got := string(n.GetCertificate().GetCsr()); got != "csr-bytes" {
					t.Errorf("Certificate.Csr = %q, want %q", got, "csr-bytes")
				}
				if got := n.GetVXLANUDPPort(); got != 4789 {
					t.Errorf("VXLANUDPPort = %d, want 4789", got)
				}
			},
		},
		{
			name: "Task",
			hex:  gogoTaskHex,
			msg:  &Task{},
			check: func(t *testing.T, m proto.Message) {
				task := m.(*Task)
				if task.GetStatus().GetState() != TaskState_RUNNING {
					t.Errorf("Status.State = %v, want %v", task.GetStatus().GetState(), TaskState_RUNNING)
				}
				mounts := task.GetSpec().GetContainer().GetMounts()
				if len(mounts) != 1 {
					t.Fatalf("len(Spec.Container.Mounts) = %d, want 1", len(mounts))
				}
				// Was a gogoproto.customtype of os.FileMode.
				if got := mounts[0].GetTmpfsOptions().GetMode(); got != 0o755 {
					t.Errorf("Mounts[0].TmpfsOptions.Mode = %#o, want %#o", got, 0o755)
				}
			},
		},
		{
			name: "Service",
			hex:  gogoServiceHex,
			msg:  &Service{},
			check: func(t *testing.T, m proto.Message) {
				svc := m.(*Service)
				// UpdateConfig.Delay was a gogoproto.stdduration time.Duration.
				if got := svc.GetSpec().GetUpdate().GetDelay().AsDuration().Seconds(); got != 3 {
					t.Errorf("Spec.Update.Delay = %vs, want 3s", got)
				}
				if got := svc.GetSpec().GetUpdate().GetMonitor().AsDuration().Seconds(); got != 9 {
					t.Errorf("Spec.Update.Monitor = %vs, want 9s", got)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, err := hex.DecodeString(tc.hex)
			if err != nil {
				t.Fatalf("decode fixture: %v", err)
			}
			if err := proto.Unmarshal(want, tc.msg); err != nil {
				t.Fatalf("unmarshal gogo payload: %v", err)
			}
			tc.check(t, tc.msg)

			// gogo's generated marshaller sorted map keys, so ask for the same
			// ordering before comparing.
			got, err := proto.MarshalOptions{Deterministic: true}.Marshal(tc.msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !bytes.Equal(want, got) {
				t.Errorf("re-encoded payload differs from the gogo encoding\n want: %s\n  got: %s",
					hex.EncodeToString(want), hex.EncodeToString(got))
			}
		})
	}
}
