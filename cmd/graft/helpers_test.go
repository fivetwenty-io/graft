package main

import (
	"reflect"
	"testing"
)

// wantEqual fails t when got and want differ.
func wantEqual(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
