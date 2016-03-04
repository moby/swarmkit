package agent

import "math"

import "testing"

func TestManagersSimple(t *testing.T) {
	addrs := []string{"one", "two", "three"}
	managers := NewManagers(addrs...)
	index := managers.Weights()

	seen := make(map[string]int)
	for i := 0; i < len(addrs)*10; i++ {
		next, err := managers.Select()
		if err != nil {
			t.Fatalf("error selecting manager: %v", err)
		}

		if _, ok := index[next]; !ok {
			t.Fatalf("unexpected manager returned: %q", next)
		}
		seen[next]++
	}

	for _, addr := range addrs {
		if _, ok := seen[addr]; !ok {
			t.Fatalf("%q not returned after several selection attempts", addr)
		}
	}

	weights := managers.Weights()
	var value float64
	for addr := range seen {
		weight, ok := weights[addr]
		if !ok {
			t.Fatalf("unexpected manager returned: %v", addr)
		}

		if weight <= 0 {
			t.Fatalf("weight should not be zero or less: %v (%v)", weight, managers.Weights())
		}

		if value == 0 {
			// sets benchmark weight, they should all be the same
			value = weight
			continue
		}

		if weight != value {
			t.Fatalf("all weights should be same %q: %v != %v, %v", addr, weight, value, weights)
		}
	}
}

func TestManagersEmpty(t *testing.T) {
	managers := NewManagers()

	_, err := managers.Select()
	if err != errManagersUnavailable {
		t.Fatalf("unexpected return from Select: %v", err)
	}

}

// TestManagersConvergence ensures that as we get positive observations,
// the actual weight increases or converges to a value higher than the initial
// value.
func TestManagersConvergence(t *testing.T) {
	managers := NewManagers()
	managers.Observe("one", 1)

	// zero weighted against 1
	if managers.Weights()["one"] < managerWeightSmoothingFactor {
		t.Fatalf("unexpected weight: %v < %v", managers.Weights()["one"], managerWeightSmoothingFactor)
	}

	// crank it up
	for i := 0; i < 10; i++ {
		managers.Observe("one", 1)
	}

	if managers.Weights()["one"] < managerWeightSmoothingFactor {
		t.Fatalf("did not converge towards 1: %v < %v", managers.Weights()["one"], managerWeightSmoothingFactor)
	}

	if managers.Weights()["one"] > 1.0 {
		t.Fatalf("should never go over towards 1: %v > %v", managers.Weights()["one"], 1.0)
	}

	// provided a poor review
	managers.Observe("one", -1)

	if managers.Weights()["one"] > 0 {
		t.Fatalf("should be below zero: %v", managers.Weights()["one"])
	}

	// The manager should be heavily downweighted but not completely to -1
	expected := (-managerWeightSmoothingFactor + (1 - managerWeightSmoothingFactor))
	epsilon := -1e-5
	if managers.Weights()["one"] < expected+epsilon {
		t.Fatalf("weight should not drop so quickly: %v < %v", managers.Weights()["one"], expected)
	}
}

func TestManagersZeroWeights(t *testing.T) {
	managers := NewManagers()
	addrs := []string{"one", "two", "three"}
	for _, addr := range addrs {
		managers.Observe(addr, 0)
	}

	seen := map[string]struct{}{}
	for i := 0; i < 10; i++ {
		addr, err := managers.Select()
		if err != nil {
			t.Fatalf("unexpected error from Select: %v", err)
		}

		seen[addr] = struct{}{}
	}

	for addr := range managers.Weights() {
		if _, ok := seen[addr]; !ok {
			t.Fatalf("manager not returned after several tries: %v (seen: %v)", addr, seen)
		}
	}

	// Pump up number 3!
	managers.Observe("three", 10)

	count := map[string]int{}
	for i := 0; i < 10; i++ {
		// basically, we expect the same one to return
		addr, err := managers.Select()
		if err != nil {
			t.Fatalf("unexpected error from Select: %v", err)
		}

		count[addr]++

		// keep observing three
		managers.Observe("three", 10)
	}

	// here, we ensure that three is at least three times more likely to be
	// selected. This is somewhat arbitrary.
	if count["three"] <= count["one"]*3 || count["three"] <= count["two"] {
		t.Fatalf("three should outpace one and two")
	}
}

func TestManagersLargeRanges(t *testing.T) {
	addrs := []string{"one", "two", "three"}
	index := make(map[string]struct{}, len(addrs))
	managers := NewManagers(addrs...)

	for _, addr := range addrs {
		index[addr] = struct{}{}
	}

	managers.Observe(addrs[0], math.NaN())
	managers.Observe(addrs[1], math.Inf(1))
	managers.Observe(addrs[2], math.Inf(-1))
	managers.Observe(addrs[2], 1) // three bounces back!

	seen := make(map[string]int)
	for i := 0; i < len(addrs)*30; i++ {
		next, err := managers.Select()
		if err != nil {
			t.Fatalf("error selecting manager: %v", err)
		}

		if _, ok := index[next]; !ok {
			t.Fatalf("unexpected manager returned: %q", next)
		}
		seen[next]++
	}

	for _, addr := range addrs {
		if _, ok := seen[addr]; !ok {
			t.Fatalf("%q not returned after several selection attempts, %v", addr, managers)
		}
	}

	for addr := range seen {
		if _, ok := index[addr]; !ok {
			t.Fatalf("unexpected manager returned: %v", addr)
		}
	}
}

var addrs = []string{
	"one", "two", "three",
	"four", "five", "six",
	"seven0", "eight0", "nine0",
	"seven1", "eight1", "nine1",
	"seven2", "eight2", "nine2",
	"seven3", "eight3", "nine3",
	"seven4", "eight4", "nine4",
	"seven5", "eight5", "nine5",
	"seven6", "eight6", "nine6"}

func BenchmarkManagersSelect3(b *testing.B) {
	benchmarkManagersSelect(b, addrs[:3]...)
}

func BenchmarkManagersSelect5(b *testing.B) {
	benchmarkManagersSelect(b, addrs[:5]...)
}

func BenchmarkManagersSelect9(b *testing.B) {
	benchmarkManagersSelect(b, addrs[:9]...)
}

func BenchmarkManagersSelect27(b *testing.B) {
	benchmarkManagersSelect(b, addrs[:27]...)
}

func benchmarkManagersSelect(b *testing.B, addrs ...string) {
	managers := NewManagers(addrs...)

	for i := 0; i < b.N; i++ {
		_, err := managers.Select()
		if err != nil {
			b.Fatalf("error selecting manager: %v", err)
		}
	}
}

func BenchmarkManagersObserve3(b *testing.B) {
	benchmarkManagersObserve(b, addrs[:3]...)
}

func BenchmarkManagersObserve5(b *testing.B) {
	benchmarkManagersObserve(b, addrs[:5]...)
}

func BenchmarkManagersObserve9(b *testing.B) {
	benchmarkManagersObserve(b, addrs[:9]...)
}

func BenchmarkManagersObserve27(b *testing.B) {
	benchmarkManagersObserve(b, addrs[:27]...)
}

func benchmarkManagersObserve(b *testing.B, addrs ...string) {
	managers := NewManagers(addrs...)

	for i := 0; i < b.N; i++ {
		managers.Observe(addrs[i%len(addrs)], 1.0)
	}
}
