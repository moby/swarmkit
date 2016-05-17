package picker

import (
	"math"
	"testing"
)

func TestRemotesSimple(t *testing.T) {
	addrs := []string{"one", "two", "three"}
	remotes := NewRemotes(addrs...)
	index := remotes.Weights()

	seen := make(map[string]int)
	for i := 0; i < len(addrs)*10; i++ {
		next, err := remotes.Select()
		if err != nil {
			t.Fatalf("error selecting remote: %v", err)
		}

		if _, ok := index[next]; !ok {
			t.Fatalf("unexpected remote returned: %q", next)
		}
		seen[next]++
	}

	for _, addr := range addrs {
		if _, ok := seen[addr]; !ok {
			t.Fatalf("%q not returned after several selection attempts", addr)
		}
	}

	weights := remotes.Weights()
	var value int
	for addr := range seen {
		weight, ok := weights[addr]
		if !ok {
			t.Fatalf("unexpected remote returned: %v", addr)
		}

		if weight <= 0 {
			t.Fatalf("weight should not be zero or less: %v (%v)", weight, remotes.Weights())
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

func TestRemotesEmpty(t *testing.T) {
	remotes := NewRemotes()

	_, err := remotes.Select()
	if err != errRemotesUnavailable {
		t.Fatalf("unexpected return from Select: %v", err)
	}

}

// TestRemotesConvergence ensures that as we get positive observations,
// the actual weight increases or converges to a value higher than the initial
// value.
func TestRemotesConvergence(t *testing.T) {
	remotes := NewRemotes()
	remotes.Observe("one", 1)

	// zero weighted against 1
	if float64(remotes.Weights()["one"]) < remoteWeightSmoothingFactor {
		t.Fatalf("unexpected weight: %v < %v", remotes.Weights()["one"], remoteWeightSmoothingFactor)
	}

	// crank it up
	for i := 0; i < 10; i++ {
		remotes.Observe("one", 1)
	}

	if float64(remotes.Weights()["one"]) < remoteWeightSmoothingFactor {
		t.Fatalf("did not converge towards 1: %v < %v", remotes.Weights()["one"], remoteWeightSmoothingFactor)
	}

	if remotes.Weights()["one"] > remoteWeightMax {
		t.Fatalf("should never go over towards %v: %v > %v", remoteWeightMax, remotes.Weights()["one"], 1.0)
	}

	// provided a poor review
	remotes.Observe("one", -1)

	if remotes.Weights()["one"] > 0 {
		t.Fatalf("should be below zero: %v", remotes.Weights()["one"])
	}

	// The remote should be heavily downweighted but not completely to -1
	expected := (-remoteWeightSmoothingFactor + (1 - remoteWeightSmoothingFactor))
	epsilon := -1e-5
	if float64(remotes.Weights()["one"]) < expected+epsilon {
		t.Fatalf("weight should not drop so quickly: %v < %v", remotes.Weights()["one"], expected)
	}
}

func TestRemotesZeroWeights(t *testing.T) {
	remotes := NewRemotes()
	addrs := []string{"one", "two", "three"}
	for _, addr := range addrs {
		remotes.Observe(addr, 0)
	}

	seen := map[string]struct{}{}
	for i := 0; i < 25; i++ {
		addr, err := remotes.Select()
		if err != nil {
			t.Fatalf("unexpected error from Select: %v", err)
		}

		seen[addr] = struct{}{}
	}

	for addr := range remotes.Weights() {
		if _, ok := seen[addr]; !ok {
			t.Fatalf("remote not returned after several tries: %v (seen: %v)", addr, seen)
		}
	}

	// Pump up number 3!
	remotes.Observe("three", 10)

	count := map[string]int{}
	for i := 0; i < 100; i++ {
		// basically, we expect the same one to return
		addr, err := remotes.Select()
		if err != nil {
			t.Fatalf("unexpected error from Select: %v", err)
		}

		count[addr]++

		// keep observing three
		remotes.Observe("three", 10)
	}

	// here, we ensure that three is at least three times more likely to be
	// selected. This is somewhat arbitrary.
	if count["three"] <= count["one"]*3 || count["three"] <= count["two"] {
		t.Fatalf("three should outpace one and two")
	}
}

func TestRemotesLargeRanges(t *testing.T) {
	addrs := []string{"one", "two", "three"}
	index := make(map[string]struct{}, len(addrs))
	remotes := NewRemotes(addrs...)

	for _, addr := range addrs {
		index[addr] = struct{}{}
	}

	remotes.Observe(addrs[0], 0)
	remotes.Observe(addrs[1], math.MaxInt64)
	remotes.Observe(addrs[2], math.MinInt64)
	remotes.Observe(addrs[2], remoteWeightMax) // three bounces back!

	seen := make(map[string]int)
	for i := 0; i < len(addrs)*remoteWeightMax*4; i++ {
		next, err := remotes.Select()
		if err != nil {
			t.Fatalf("error selecting remote: %v", err)
		}

		if _, ok := index[next]; !ok {
			t.Fatalf("unexpected remote returned: %q", next)
		}
		seen[next]++
	}

	for _, addr := range addrs {
		if _, ok := seen[addr]; !ok {
			t.Fatalf("%q not returned after several selection attempts, %v", addr, remotes)
		}
	}

	for addr := range seen {
		if _, ok := index[addr]; !ok {
			t.Fatalf("unexpected remote returned: %v", addr)
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

func BenchmarkRemotesSelect3(b *testing.B) {
	benchmarkRemotesSelect(b, addrs[:3]...)
}

func BenchmarkRemotesSelect5(b *testing.B) {
	benchmarkRemotesSelect(b, addrs[:5]...)
}

func BenchmarkRemotesSelect9(b *testing.B) {
	benchmarkRemotesSelect(b, addrs[:9]...)
}

func BenchmarkRemotesSelect27(b *testing.B) {
	benchmarkRemotesSelect(b, addrs[:27]...)
}

func benchmarkRemotesSelect(b *testing.B, addrs ...string) {
	remotes := NewRemotes(addrs...)

	for i := 0; i < b.N; i++ {
		_, err := remotes.Select()
		if err != nil {
			b.Fatalf("error selecting remote: %v", err)
		}
	}
}

func BenchmarkRemotesObserve3(b *testing.B) {
	benchmarkRemotesObserve(b, addrs[:3]...)
}

func BenchmarkRemotesObserve5(b *testing.B) {
	benchmarkRemotesObserve(b, addrs[:5]...)
}

func BenchmarkRemotesObserve9(b *testing.B) {
	benchmarkRemotesObserve(b, addrs[:9]...)
}

func BenchmarkRemotesObserve27(b *testing.B) {
	benchmarkRemotesObserve(b, addrs[:27]...)
}

func benchmarkRemotesObserve(b *testing.B, addrs ...string) {
	remotes := NewRemotes(addrs...)

	for i := 0; i < b.N; i++ {
		remotes.Observe(addrs[i%len(addrs)], 1.0)
	}
}
