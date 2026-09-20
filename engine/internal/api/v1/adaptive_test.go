package v1

import (
	"encoding/json"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/proxy"
	"slices"
	"testing"
	"time"
)

func TestAdaptiveSchedulingContract(t *testing.T) {
	hello := NewHelloResult("test", "test", "", 0, false, time.Time{})
	if !slices.Contains(hello.SchedulingStrategies, proxy.StrategyAdaptive) {
		t.Fatal("strategy not advertised")
	}
	if slices.Contains(hello.Capabilities, proxy.StrategyAdaptive) {
		t.Fatal("strategy incorrectly advertised as method")
	}
	var params EngineStartParams
	if err := json.Unmarshal([]byte(`{"strategy":"adaptive-throughput","weighted":false}`), &params); err != nil {
		t.Fatal(err)
	}
	if params.ProxyConfig().Strategy != proxy.StrategyAdaptive {
		t.Fatal("start dropped strategy")
	}
}
