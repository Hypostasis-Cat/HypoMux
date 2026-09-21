package v1

import (
	"encoding/json"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/proxy"
	"slices"
	"testing"
	"time"
)

func TestLatencySchedulingContract(t *testing.T) {
	hello := NewHelloResult("test", "test", "", 0, false, time.Time{})
	if !slices.Contains(hello.SchedulingStrategies, proxy.StrategyLatency) {
		t.Fatal("latency strategy not advertised")
	}
	var params EngineStartParams
	if err := json.Unmarshal([]byte(`{"strategy":"latency-first","weighted":false}`), &params); err != nil {
		t.Fatal(err)
	}
	if params.ProxyConfig().Strategy != proxy.StrategyLatency {
		t.Fatal("start lost latency strategy")
	}
	if got, err := proxy.NormalizeStrategy(proxy.StrategyLatency, true); err != nil || got != proxy.StrategyLatency {
		t.Fatal(got, err)
	}
}
