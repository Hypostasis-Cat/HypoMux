package services

import "testing"

func TestParseSingBoxMajorMinor(t *testing.T) {
	tests := []struct {
		output     string
		major      int
		minor      int
		shouldFail bool
	}{
		{output: "sing-box version 1.13.21\n", major: 1, minor: 13},
		{output: "sing-box version 1.14.0-rc.5\n", major: 1, minor: 14},
		{output: "unexpected", shouldFail: true},
	}
	for _, test := range tests {
		major, minor, err := parseSingBoxMajorMinor(test.output)
		if test.shouldFail {
			if err == nil {
				t.Fatalf("parseSingBoxMajorMinor(%q) unexpectedly succeeded", test.output)
			}
			continue
		}
		if err != nil || major != test.major || minor != test.minor {
			t.Fatalf("parseSingBoxMajorMinor(%q) = %d, %d, %v", test.output, major, minor, err)
		}
	}
}
