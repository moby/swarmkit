package picker

import (
	"math"
	"testing"

	"github.com/docker/swarmkit/api"
)

func TestRemotesSimple(t *testing.T) {
	peers := []api.Peer{{Addr: "one"}, {Addr: "two"}, {Addr: "three"}}
	remotes := NewRemotes(peers...)
	index := remotes.Weights()

	seen := make(map[api.Peer]int)
	for i := 0; i < len(peers)*10; i++ {
		next, err := remotes.Select()
		if err != nil {
			t.Fatalf("error selecting remote: %v", err)
		}

		if _, ok := index[next]; !ok {
			t.Fatalf("unexpected remote returned: %q", next)
		}
		seen[next]++
	}

	for _, peer := range peers {
		if _, ok := seen[peer]; !ok {
			t.Fatalf("%q not returned after several selection attempts", peer)
		}
	}

	weights := remotes.Weights()
	var value int
	for peer := range seen {
		weight, ok := weights[peer]
		if !ok {
			t.Fatalf("unexpected remote returned: %v", peer)
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
			t.Fatalf("all weights should be same %q: %v != %v, %v", peer, weight, value, weights)
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

func TestRemotesExclude(t *testing.T) {
	peers := []api.Peer{{Addr: "one"}, {Addr: "two"}, {Addr: "three"}}
	excludes := []string{"one", "two", "three"}
	remotes := NewRemotes(peers...)

	// exclude all
	_, err := remotes.Select(excludes...)
	if err != errRemotesUnavailable {
		t.Fatal("select an excluded peer")
	}

	// exclude one peer
	for i := 0; i < len(peers)*10; i++ {
		next, err := remotes.Select(excludes[0])
		if err != nil {
			t.Fatalf("error selecting remote: %v", err)
		}

		if next == peers[0] {
			t.Fatal("select an excluded peer")
		}
	}

	// exclude 2 peers
	for i := 0; i < len(peers)*10; i++ {
		next, err := remotes.Select(excludes[1:]...)
		if err != nil {
			t.Fatalf("error selecting remote: %v", err)
		}

		if next != peers[0] {
			t.Fatalf("select an excluded peer: %v", next)
		}
	}
}

// TestRemotesConvergence ensures that as we get positive observations,
// the actual weight increases or converges to a value higher than the initial
// value.
func TestRemotesConvergence(t *testing.T) {
	remotes := NewRemotes()
	remotes.Observe(api.Peer{Addr: "one"}, 1)

	// zero weighted against 1
	if float64(remotes.Weights()[api.Peer{Addr: "one"}]) < remoteWeightSmoothingFactor {
		t.Fatalf("unexpected weight: %v < %v", remotes.Weights()[api.Peer{Addr: "one"}], remoteWeightSmoothingFactor)
	}

	// crank it up
	for i := 0; i < 10; i++ {
		remotes.Observe(api.Peer{Addr: "one"}, 1)
	}

	if float64(remotes.Weights()[api.Peer{Addr: "one"}]) < remoteWeightSmoothingFactor {
		t.Fatalf("did not converge towards 1: %v < %v", remotes.Weights()[api.Peer{Addr: "one"}], remoteWeightSmoothingFactor)
	}

	if remotes.Weights()[api.Peer{Addr: "one"}] > remoteWeightMax {
		t.Fatalf("should never go over towards %v: %v > %v", remoteWeightMax, remotes.Weights()[api.Peer{Addr: "one"}], 1.0)
	}

	// provided a poor review
	remotes.Observe(api.Peer{Addr: "one"}, -1)

	if remotes.Weights()[api.Peer{Addr: "one"}] > 0 {
		t.Fatalf("should be below zero: %v", remotes.Weights()[api.Peer{Addr: "one"}])
	}

	// The remote should be heavily downweighted but not completely to -1
	expected := (-remoteWeightSmoothingFactor + (1 - remoteWeightSmoothingFactor))
	epsilon := -1e-5
	if float64(remotes.Weights()[api.Peer{Addr: "one"}]) < expected+epsilon {
		t.Fatalf("weight should not drop so quickly: %v < %v", remotes.Weights()[api.Peer{Addr: "one"}], expected)
	}
}

func TestRemotesZeroWeights(t *testing.T) {
	remotes := NewRemotes()
	peers := []api.Peer{{Addr: "one"}, {Addr: "two"}, {Addr: "three"}}
	for _, peer := range peers {
		remotes.Observe(peer, 0)
	}

	seen := map[api.Peer]struct{}{}
	for i := 0; i < 25; i++ {
		peer, err := remotes.Select()
		if err != nil {
			t.Fatalf("unexpected error from Select: %v", err)
		}

		seen[peer] = struct{}{}
	}

	for peer := range remotes.Weights() {
		if _, ok := seen[peer]; !ok {
			t.Fatalf("remote not returned after several tries: %v (seen: %v)", peer, seen)
		}
	}

	// Pump up number 3!
	remotes.Observe(api.Peer{Addr: "three"}, 10)

	count := map[api.Peer]int{}
	for i := 0; i < 100; i++ {
		// basically, we expect the same one to return
		peer, err := remotes.Select()
		if err != nil {
			t.Fatalf("unexpected error from Select: %v", err)
		}

		count[peer]++

		// keep observing three
		remotes.Observe(api.Peer{Addr: "three"}, 10)
	}

	// here, we ensure that three is at least three times more likely to be
	// selected. This is somewhat arbitrary.
	if count[api.Peer{Addr: "three"}] <= count[api.Peer{Addr: "one"}]*3 || count[api.Peer{Addr: "three"}] <= count[api.Peer{Addr: "two"}] {
		t.Fatalf("three should outpace one and two")
	}
}

func TestRemotesLargeRanges(t *testing.T) {
	peers := []api.Peer{{Addr: "one"}, {Addr: "two"}, {Addr: "three"}}
	index := make(map[api.Peer]struct{}, len(peers))
	remotes := NewRemotes(peers...)

	for _, peer := range peers {
		index[peer] = struct{}{}
	}

	remotes.Observe(peers[0], 0)
	remotes.Observe(peers[1], math.MaxInt64)
	remotes.Observe(peers[2], math.MinInt64)
	remotes.Observe(peers[2], remoteWeightMax) // three bounces back!

	seen := make(map[api.Peer]int)
	for i := 0; i < len(peers)*remoteWeightMax*4; i++ {
		next, err := remotes.Select()
		if err != nil {
			t.Fatalf("error selecting remote: %v", err)
		}

		if _, ok := index[next]; !ok {
			t.Fatalf("unexpected remote returned: %q", next)
		}
		seen[next]++
	}

	for _, peer := range peers {
		if _, ok := seen[peer]; !ok {
			t.Fatalf("%q not returned after several selection attempts, %v", peer, remotes)
		}
	}

	for peer := range seen {
		if _, ok := index[peer]; !ok {
			t.Fatalf("unexpected remote returned: %v", peer)
		}
	}
}

var peers = []api.Peer{
	{Addr: "one"}, {Addr: "two"}, {Addr: "three"},
	{Addr: "four"}, {Addr: "five"}, {Addr: "six"},
	{Addr: "seven0"}, {Addr: "eight0"}, {Addr: "nine0"},
	{Addr: "seven1"}, {Addr: "eight1"}, {Addr: "nine1"},
	{Addr: "seven2"}, {Addr: "eight2"}, {Addr: "nine2"},
	{Addr: "seven3"}, {Addr: "eight3"}, {Addr: "nine3"},
	{Addr: "seven4"}, {Addr: "eight4"}, {Addr: "nine4"},
	{Addr: "seven5"}, {Addr: "eight5"}, {Addr: "nine5"},
	{Addr: "seven6"}, {Addr: "eight6"}, {Addr: "nine6"}}

func BenchmarkRemotesSelect3(b *testing.B) {
	benchmarkRemotesSelect(b, peers[:3]...)
}

func BenchmarkRemotesSelect5(b *testing.B) {
	benchmarkRemotesSelect(b, peers[:5]...)
}

func BenchmarkRemotesSelect9(b *testing.B) {
	benchmarkRemotesSelect(b, peers[:9]...)
}

func BenchmarkRemotesSelect27(b *testing.B) {
	benchmarkRemotesSelect(b, peers[:27]...)
}

func benchmarkRemotesSelect(b *testing.B, peers ...api.Peer) {
	remotes := NewRemotes(peers...)

	for i := 0; i < b.N; i++ {
		_, err := remotes.Select()
		if err != nil {
			b.Fatalf("error selecting remote: %v", err)
		}
	}
}

func BenchmarkRemotesObserve3(b *testing.B) {
	benchmarkRemotesObserve(b, peers[:3]...)
}

func BenchmarkRemotesObserve5(b *testing.B) {
	benchmarkRemotesObserve(b, peers[:5]...)
}

func BenchmarkRemotesObserve9(b *testing.B) {
	benchmarkRemotesObserve(b, peers[:9]...)
}

func BenchmarkRemotesObserve27(b *testing.B) {
	benchmarkRemotesObserve(b, peers[:27]...)
}

func benchmarkRemotesObserve(b *testing.B, peers ...api.Peer) {
	remotes := NewRemotes(peers...)

	for i := 0; i < b.N; i++ {
		remotes.Observe(peers[i%len(peers)], 1.0)
	}
}
