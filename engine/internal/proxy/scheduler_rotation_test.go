package proxy

import "testing"

func TestConstrainedFlowsPreservePoolRotation(t *testing.T) {
	s := newScheduler([]Adapter{{Name: "a"}, {Name: "b"}}, false)
	counts := map[string]int{}
	for range 20 {
		selected, ok := s.Select(nil)
		if !ok {
			t.Fatal("pool selection failed")
		}
		counts[selected.Name]++
		constrained, ok := s.Select(map[string]struct{}{"a": {}})
		if !ok || constrained.Name != "b" {
			t.Fatal("constraint ignored")
		}
	}
	if counts["a"] != 10 || counts["b"] != 10 {
		t.Fatalf("constrained flows biased pool rotation: %v", counts)
	}
}
