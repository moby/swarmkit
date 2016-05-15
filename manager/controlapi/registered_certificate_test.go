package controlapi

import (
	"reflect"
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func TestGetRegisteredCertificate(t *testing.T) {
	ts := newTestServer(t)

	c1 := &api.RegisteredCertificate{
		ID:   "id1",
		CN:   "cn1",
		Role: ca.ManagerRole,
		Status: api.IssuanceStatus{
			State: api.IssuanceStatePending,
		},
	}

	assert.NoError(t, ts.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateRegisteredCertificate(tx, c1))
		return nil
	}))

	_, err := ts.Client.GetRegisteredCertificate(context.Background(), &api.GetRegisteredCertificateRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.GetRegisteredCertificate(context.Background(), &api.GetRegisteredCertificateRequest{RegisteredCertificateID: "invalid"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	r, err := ts.Client.GetRegisteredCertificate(context.Background(), &api.GetRegisteredCertificateRequest{RegisteredCertificateID: c1.ID})
	assert.NoError(t, err)
	assert.Equal(t, c1.ID, r.RegisteredCertificate.ID)
}

func TestListRegisteredCertificates(t *testing.T) {
	ts := newTestServer(t)

	c1 := &api.RegisteredCertificate{
		ID:   "id1",
		CN:   "cn1",
		Role: ca.ManagerRole,
		Status: api.IssuanceStatus{
			State: api.IssuanceStatePending,
		},
	}

	c2 := &api.RegisteredCertificate{
		ID:   "id2",
		CN:   "cn2",
		Role: ca.ManagerRole,
		Status: api.IssuanceStatus{
			State: api.IssuanceStateIssued,
		},
	}

	assert.NoError(t, ts.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateRegisteredCertificate(tx, c1))
		assert.NoError(t, store.CreateRegisteredCertificate(tx, c2))
		return nil
	}))

	r1, err := ts.Client.ListRegisteredCertificates(context.Background(), &api.ListRegisteredCertificatesRequest{
		State: []api.IssuanceState{api.IssuanceStatePending},
	})
	assert.NoError(t, err)
	assert.Len(t, r1.Certificates, 1)
	assert.Equal(t, r1.Certificates[0].ID, "id1")

	r2, err := ts.Client.ListRegisteredCertificates(context.Background(), &api.ListRegisteredCertificatesRequest{
		State: []api.IssuanceState{api.IssuanceStateIssued},
	})
	assert.NoError(t, err)
	assert.Len(t, r1.Certificates, 1)
	assert.Equal(t, r2.Certificates[0].ID, "id2")

	r3, err := ts.Client.ListRegisteredCertificates(context.Background(), &api.ListRegisteredCertificatesRequest{
		State: []api.IssuanceState{api.IssuanceStateBlocked},
	})
	assert.NoError(t, err)
	assert.Len(t, r3.Certificates, 0)

	r4, err := ts.Client.ListRegisteredCertificates(context.Background(), &api.ListRegisteredCertificatesRequest{})
	assert.NoError(t, err)
	assert.Len(t, r4.Certificates, 2)
}

func TestUpdateRegisteredCertificate(t *testing.T) {
	ts := newTestServer(t)

	// Approve a pending certificate

	c1 := &api.RegisteredCertificate{
		ID:   "id1",
		CN:   "cn1",
		Role: ca.ManagerRole,
		Status: api.IssuanceStatus{
			State: api.IssuanceStatePending,
		},
	}

	assert.NoError(t, ts.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateRegisteredCertificate(tx, c1))
		return nil
	}))

	resp, err := ts.Client.UpdateRegisteredCertificate(context.Background(), &api.UpdateRegisteredCertificateRequest{
		RegisteredCertificateID:      "id1",
		RegisteredCertificateVersion: &c1.Meta.Version,
		Spec: &api.RegisteredCertificateSpec{
			DesiredState: api.IssuanceStateIssued,
		},
	})
	assert.NoError(t, err)

	ts.Store.View(func(readTx store.ReadTx) {
		cert := store.GetRegisteredCertificate(readTx, "id1")
		assert.Equal(t, api.IssuanceStateIssued, cert.Spec.DesiredState)
		if !reflect.DeepEqual(cert, resp.RegisteredCertificate) {
			t.Fatal("received certificate does not match store")
		}
	})

	// Reject a pending certificate

	c2 := &api.RegisteredCertificate{
		ID:   "id2",
		CN:   "cn2",
		Role: ca.ManagerRole,
		Status: api.IssuanceStatus{
			State: api.IssuanceStatePending,
		},
	}

	assert.NoError(t, ts.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateRegisteredCertificate(tx, c2))
		return nil
	}))

	resp, err = ts.Client.UpdateRegisteredCertificate(context.Background(), &api.UpdateRegisteredCertificateRequest{
		RegisteredCertificateID:      "id2",
		RegisteredCertificateVersion: &c2.Meta.Version,
		Spec: &api.RegisteredCertificateSpec{
			DesiredState: api.IssuanceStateRejected,
		},
	})
	assert.NoError(t, err)

	ts.Store.View(func(readTx store.ReadTx) {
		cert := store.GetRegisteredCertificate(readTx, "id2")
		assert.Equal(t, api.IssuanceStateRejected, cert.Spec.DesiredState)
		if !reflect.DeepEqual(cert, resp.RegisteredCertificate) {
			t.Fatal("received certificate does not match store")
		}
	})
}
