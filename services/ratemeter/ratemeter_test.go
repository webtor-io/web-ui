package ratemeter

import (
	"testing"
	"time"
)

func TestMeter(t *testing.T) {
	m := New(0.5)
	t0 := time.Unix(1000, 0)
	if r := m.Sample(100, t0); r != 0 {
		t.Fatalf("first sample only primes, got %v", r)
	}
	// 1000 bytes in one second → 1000 B/s; the first interval is taken as is.
	if r := m.Sample(1100, t0.Add(time.Second)); r != 1000 {
		t.Fatalf("first interval: got %v", r)
	}
	// A 4000-byte tick is smoothed halfway toward, not adopted wholesale.
	if r := m.Sample(5100, t0.Add(2*time.Second)); r != 2500 {
		t.Fatalf("smoothing: got %v", r)
	}
	// A quiet tick decays instead of dropping to zero.
	if r := m.Sample(5100, t0.Add(3*time.Second)); r != 1250 {
		t.Fatalf("decay: got %v", r)
	}
	// Counter going backwards (re-check) re-primes and keeps the last rate.
	if r := m.Sample(10, t0.Add(4*time.Second)); r != 1250 {
		t.Fatalf("re-prime: got %v", r)
	}
	if r := m.Sample(1010, t0.Add(5*time.Second)); r != 1125 {
		t.Fatalf("after re-prime: got %v", r)
	}
	// Same timestamp twice must not divide by zero.
	if r := m.Sample(2000, t0.Add(5*time.Second)); r != 1125 {
		t.Fatalf("zero interval: got %v", r)
	}
}

// Events arrive in bursts: a 4 MiB piece landing 10 ms after the previous
// sample must not read as 400 MB/s. The burst is folded into the next
// measured interval instead.
func TestMeter_BurstsAreNotSpikes(t *testing.T) {
	m := New(0.4)
	t0 := time.Unix(1000, 0)
	m.Sample(0, t0)
	m.Sample(1_000_000, t0.Add(time.Second)) // 1 MB/s
	if r := m.Sample(5_000_000, t0.Add(1010*time.Millisecond)); r != 1_000_000 {
		t.Fatalf("burst sample must not be measured: got %v", r)
	}
	// One second after the last measured sample: 4 MB over 1 s, smoothed.
	if r := m.Sample(5_000_000, t0.Add(2*time.Second)); r != 0.4*4_000_000+0.6*1_000_000 {
		t.Fatalf("folded burst: got %v", r)
	}
}

// A counter jump beyond anything a swarm can deliver is a re-check or a pod
// swap, not throughput: it re-primes and leaves the average alone.
func TestMeter_ImpossibleJumpReprimes(t *testing.T) {
	m := New(0.4)
	t0 := time.Unix(1000, 0)
	m.Sample(0, t0)
	m.Sample(2_000_000, t0.Add(time.Second)) // 2 MB/s
	if r := m.Sample(2_000_000+500_000_000, t0.Add(2*time.Second)); r != 2_000_000 {
		t.Fatalf("500 MB/s jump must not enter the average: got %v", r)
	}
	if r := m.Sample(2_000_000+500_000_000+1_000_000, t0.Add(3*time.Second)); r != 0.4*1_000_000+0.6*2_000_000 {
		t.Fatalf("measuring resumes from the jump: got %v", r)
	}
}
