package version

import "testing"

func TestCoreVersionIsSet(t *testing.T) {
	if Core == "" {
		t.Fatal("Core version is empty")
	}
}
