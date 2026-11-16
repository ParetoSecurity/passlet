package passlet

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("PASSLET_VM_ROLE") != "" || os.Getenv("PASSLET_LIVE_TEST") != "" {
		os.Exit(m.Run())
	}
	home, err := os.MkdirTemp("", "passlet-tests-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
