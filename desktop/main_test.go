package main

import "testing"

func TestHasArgument(t *testing.T) {
	if !hasArgument([]string{"--silent", "--recover-network"}, "--recover-network") {
		t.Fatal("expected recovery argument to be found")
	}
	if hasArgument([]string{"--silent"}, "--recover-network") {
		t.Fatal("unexpected recovery argument")
	}
}
