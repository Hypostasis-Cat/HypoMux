package startup

import (
	"reflect"
	"testing"
)

func TestStandardUIRelaunchArgumentsPreserveLaunchIntent(t *testing.T) {
	input := []string{"--silent", "--custom=value", standardUIRelaunchArgument}
	want := []string{"--silent", "--custom=value", standardUIRelaunchArgument}
	got := standardUIRelaunchArguments(input)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected relaunch arguments: got %#v want %#v", got, want)
	}
	if !hasStandardUIRelaunchArgument(got) {
		t.Fatal("relaunch marker was not preserved")
	}
}

func TestCombineLaunchDetailsSkipsEmptyValues(t *testing.T) {
	if got := combineLaunchDetails("first", " ", "second"); got != "first; second" {
		t.Fatalf("unexpected launch detail: %q", got)
	}
}
