package passlet

import (
	"os"
	"passlet.dev/passlet/testutil/testkeys"
	"testing"
)

func TestEndpointPrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PASSLET_ENDPOINT", "")
	check := func(explicit, want string) {
		t.Helper()
		got, err := ResolveEndpoint(explicit)
		if err != nil || got != want {
			t.Fatalf("%q %v, want %q", got, err, want)
		}
	}
	check("", "unix://"+DefaultSocket())
	testkeys.Write(t, os.Getenv("HOME"))
	if err := testkeys.SaveEndpoint("https://saved.example:17400"); err != nil {
		t.Fatal(err)
	}
	check("", "https://saved.example:17400")
	t.Setenv("PASSLET_ENDPOINT", "https://environment.example:17400")
	check("", "https://environment.example:17400")
	check("https://explicit.example:17400", "https://explicit.example:17400")
	t.Setenv("PASSLET_ENDPOINT", "")
	os.WriteFile(connectionPath(), []byte("invalid"), 0600)
	if _, err := ResolveEndpoint(""); err == nil {
		t.Fatal("invalid saved pairing accepted")
	}
}
