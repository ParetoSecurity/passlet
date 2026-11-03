package passlet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEndpointPrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PASSLET_ENDPOINT", "")
	check := func(explicit, want string) {
		t.Helper()
		got, err := ResolveEndpoint(explicit)
		if err != nil || got != want {
			t.Fatalf("got %q, %v; want %q", got, err, want)
		}
	}
	check("", "unix://"+DefaultSocket())
	if err := SaveEndpoint("https://saved.example.ts.net:17400"); err != nil {
		t.Fatal(err)
	}
	check("", "https://saved.example.ts.net:17400")
	t.Setenv("PASSLET_ENDPOINT", "https://environment.example.ts.net:17400")
	check("", "https://environment.example.ts.net:17400")
	check("https://explicit.example.ts.net:17400", "https://explicit.example.ts.net:17400")
	path, _ := endpointFile()
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	check("", "https://environment.example.ts.net:17400")
	t.Setenv("PASSLET_ENDPOINT", "")
	check("unix:///tmp/explicit.sock", "unix:///tmp/explicit.sock")
	if _, err := ResolveEndpoint(""); err == nil {
		t.Fatal("invalid config silently fell back")
	}
}

func TestSavedEndpointValidation(t *testing.T) {
	for _, value := range []string{"", "http://insecure.example", "https://secret@mac.example", "https://mac.example/path", strings.Repeat("x", 4097)} {
		t.Run(value[:min(len(value), 30)], func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("PASSLET_ENDPOINT", "")
			path, _ := endpointFile()
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := ResolveEndpoint(""); err == nil {
				t.Fatal("invalid saved endpoint accepted")
			}
		})
	}
	if err := SaveEndpoint("http://insecure.example"); err == nil {
		t.Fatal("invalid endpoint saved")
	}
	if err := SaveEndpoint("https://" + strings.Repeat("x", 4096)); err == nil {
		t.Fatal("oversized endpoint saved")
	}
}

func TestSaveEndpointReplacesPrivately(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PASSLET_ENDPOINT", "")
	for _, endpoint := range []string{"https://first.example.ts.net:17400", "unix:///tmp/forwarded.sock"} {
		if err := SaveEndpoint(endpoint); err != nil {
			t.Fatal(err)
		}
		got, err := ResolveEndpoint("")
		if err != nil || got != endpoint {
			t.Fatalf("%q %v", got, err)
		}
	}
	path, _ := endpointFile()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("endpoint permissions: %v", info.Mode())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v %v", entries, err)
	}
}

func TestEndpointFilesystemFailures(t *testing.T) {
	t.Setenv("PASSLET_ENDPOINT", "")
	t.Run("missing home", func(t *testing.T) {
		t.Setenv("HOME", "")
		if _, err := ResolveEndpoint(""); err == nil {
			t.Fatal("missing home accepted")
		}
		if err := SaveEndpoint("unix:///tmp/conductor.sock"); err == nil {
			t.Fatal("saved without a home")
		}
	})
	t.Run("config directory is a file", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		if err := os.WriteFile(filepath.Join(home, ".config"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveEndpoint(""); err == nil {
			t.Fatal("unreadable config accepted")
		}
		if err := SaveEndpoint("unix:///tmp/conductor.sock"); err == nil {
			t.Fatal("saved inside a file")
		}
	})
	t.Run("endpoint is a directory", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		path, _ := endpointFile()
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveEndpoint(""); err == nil {
			t.Fatal("read directory as endpoint")
		}
		if err := SaveEndpoint("unix:///tmp/conductor.sock"); err == nil {
			t.Fatal("replaced directory")
		}
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil || len(entries) != 1 {
			t.Fatalf("temporary file left behind: %v %v", entries, err)
		}
	})
	t.Run("config directory is read only", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root bypasses filesystem permissions")
		}
		t.Setenv("HOME", t.TempDir())
		path, _ := endpointFile()
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(dir, 0700)
		if err := SaveEndpoint("unix:///tmp/conductor.sock"); err == nil {
			t.Fatal("saved in read-only directory")
		}
	})
}
