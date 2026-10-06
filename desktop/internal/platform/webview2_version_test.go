package platform

import "testing"

func TestUsableWebViewVersion(t *testing.T) {
	for _, v := range []string{"", "0.0.0.0", "garbage", "1.2", "-1.0.0.0"} {
		if usableWebViewVersion(v) {
			t.Errorf("accepted %q", v)
		}
	}
	if !usableWebViewVersion("140.0.3485.81") {
		t.Fatal("valid runtime rejected")
	}
}
