package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDiagnoseCommandEmitsCompatibleJSONForInvalidSource(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run(
		[]string{"diagnose", "--src-ip", "invalid", "--target-ip", "223.5.5.5"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}

	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if result["status"] != "unavailable" || result["note"] != "invalid --src-ip" {
		t.Fatalf("result = %#v", result)
	}
	if result["loss_rate"] != float64(100) || result["sent"] != float64(0) {
		t.Fatalf("counters = %#v", result)
	}
}
