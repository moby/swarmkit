package idm

import (
	"testing"
)

func TestNew(t *testing.T) {
	i, err := New(0, 10)
	if err != nil {
		t.Errorf("idm.New(0, 10) error = %v", err)
	}
	if i.set == nil {
		t.Error("set is not initialized")
	}
	if i.start != 0 {
		t.Errorf("unexpected start: got %d, want 0", i.start)
	}
	if i.end != 10 {
		t.Errorf("unexpected end: got %d, want 10", i.end)
	}
}

func TestAllocate(t *testing.T) {
	i, err := New(50, 52)
	if err != nil {
		t.Fatal(err)
	}

	if err = i.GetSpecificID(49); err == nil {
		t.Error("i.GetSpecificID(49): expected failure but succeeded")
	}

	if err = i.GetSpecificID(53); err == nil {
		t.Fatal("i.GetSpecificID(53): expected failure but succeeded")
	}

	o, err := i.GetID(false)
	if err != nil {
		t.Errorf("i.GetID(false) error = %v", err)
	}
	if o != 50 {
		t.Errorf("i.GetID(false) = %v, want 50", o)
	}

	err = i.GetSpecificID(50)
	if err == nil {
		t.Error("i.GetSpecificID(50): allocating already-allocated id should fail")
	}

	o, err = i.GetID(false)
	if err != nil {
		t.Errorf("i.GetID(false) error = %v", err)
	}
	if o != 51 {
		t.Errorf("i.GetID(false) = %v, want 51", o)
	}

	o, err = i.GetID(false)
	if err != nil {
		t.Errorf("i.GetID(false) error = %v", err)
	}
	if o != 52 {
		t.Errorf("i.GetID(false) = %v, want 52", o)
	}

	o, err = i.GetID(false)
	if err == nil {
		t.Errorf("i.GetID(false) = %v, allocating ID from full set should fail", o)
	}

	i.Release(50)

	o, err = i.GetID(false)
	if err != nil {
		t.Errorf("i.GetID(false) error = %v", err)
	}
	if o != 50 {
		t.Errorf("i.GetID(false) = %v, want 50", o)
	}

	i.Release(52)
	err = i.GetSpecificID(52)
	if err != nil {
		t.Errorf("i.GetSpecificID(52) error = %v, expected success allocating a released ID", err)
	}
}

func TestUninitialized(t *testing.T) {
	i := &IDM{}

	if _, err := i.GetID(false); err == nil {
		t.Error("i.GetID(...) on uninitialized set should fail")
	}

	if err := i.GetSpecificID(44); err == nil {
		t.Error("i.GetSpecificID(...) on uninitialized set should fail")
	}
}

func TestAllocateSerial(t *testing.T) {
	i, err := New(50, 55)
	if err != nil {
		t.Fatalf("New(50, 55) error = %v", err)
	}

	if err = i.GetSpecificID(49); err == nil {
		t.Errorf("i.GetSpecificID(49): allocating out-of-range id should fail")
	}

	if err = i.GetSpecificID(56); err == nil {
		t.Errorf("i.GetSpecificID(56): allocating out-of-range id should fail")
	}

	o, err := i.GetID(true)
	if err != nil {
		t.Errorf("i.GetID(true) error = %v", err)
	}
	if o != 50 {
		t.Errorf("i.GetID(true) = %v, want 50", o)
	}

	err = i.GetSpecificID(50)
	if err == nil {
		t.Errorf("i.GetSpecificID(50): allocating already-allocated id should fail")
	}

	o, err = i.GetID(true)
	if err != nil {
		t.Errorf("i.GetID(true) error = %v", err)
	}
	if o != 51 {
		t.Errorf("i.GetID(true) = %v, want 51", o)
	}

	o, err = i.GetID(true)
	if err != nil {
		t.Errorf("i.GetID(true) error = %v", err)
	}
	if o != 52 {
		t.Errorf("i.GetID(true) = %v, want 52", o)
	}

	i.Release(50)

	o, err = i.GetID(true)
	if err != nil {
		t.Errorf("i.GetID(true) error = %v", err)
	}
	if o != 53 {
		t.Errorf("i.GetID(true) = %v, want 53", o)
	}

	i.Release(52)
	err = i.GetSpecificID(52)
	if err != nil {
		t.Errorf("i.GetSpecificID(52) error = %v, expected success allocating a released ID", err)
	}
}
