package expiry

import (
	"math/rand/v2"
	"testing"
	"time"
)

func TestRenewDeleteAndExpiryBoundary(t *testing.T) {
	var x Index[string]
	now := time.Now()
	x.Set("renewed", now)
	x.Set("due", now.Add(time.Second))
	for i := 0; i < 1000; i++ {
		x.Set("renewed", now.Add(time.Hour))
	}
	if x.Len() != 2 {
		t.Fatalf("renewal grew index to %d", x.Len())
	}
	if _, ok := x.PopExpired(now); ok {
		t.Fatal("expired a renewed key")
	}
	if key, ok := x.PopExpired(now.Add(time.Second)); !ok || key != "due" {
		t.Fatalf("boundary pop: %q %v", key, ok)
	}
	x.Delete("renewed")
	x.Delete("missing")
	if x.Len() != 0 || len(x.items) != 0 {
		t.Fatal("delete retained index state")
	}
}

func TestIndexMatchesDeadlineMap(t *testing.T) {
	var x Index[int]
	want := map[int]time.Time{}
	rng := rand.New(rand.NewPCG(7, 11))
	now := time.Now()
	for step := 0; step < 5000; step++ {
		key := rng.IntN(100)
		switch rng.IntN(3) {
		case 0:
			at := now.Add(time.Duration(rng.IntN(100)-50) * time.Second)
			x.Set(key, at)
			want[key] = at
		case 1:
			x.Delete(key)
			delete(want, key)
		case 2:
			for {
				key, ok := x.PopExpired(now)
				if !ok {
					break
				}
				at, exists := want[key]
				if !exists || at.After(now) {
					t.Fatalf("unexpected expiry of %d", key)
				}
				delete(want, key)
			}
			for key, at := range want {
				if !at.After(now) {
					t.Fatalf("missed due key %d", key)
				}
			}
		}
		if x.Len() != len(want) || len(x.items) != len(want) {
			t.Fatalf("size mismatch at step %d", step)
		}
		key, at, ok := x.First()
		if ok != (len(want) > 0) {
			t.Fatal("wrong empty state")
		}
		if ok {
			if !at.Equal(want[key]) {
				t.Fatal("wrong deadline")
			}
			for _, candidate := range want {
				if candidate.Before(at) {
					t.Fatal("heap lost earliest deadline")
				}
			}
		}
	}
}
