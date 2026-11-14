package ota

import (
	"os"
	"path/filepath"
	"testing"

	"smart_home/hub/internal/config"
	"smart_home/hub/internal/store"
)

func TestVersionFromFile(t *testing.T) {
	cases := map[string]string{
		"smart_control-1.2.3.bin": "1.2.3",
		"firmware-2.0.0-rc1.bin":  "2.0.0",
		"plain.bin":               "plain",
	}
	for name, want := range cases {
		if got := VersionFromFile(name); got != want {
			t.Fatalf("%s: got %q want %q", name, got, want)
		}
	}
}

func TestListAndFilePath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a-1.0.0.bin"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b-2.0.0.bin"), []byte("bb"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(config.Config{FirmwareDir: dir}, store.New(), nil)
	files, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files=%d", len(files))
	}
	if _, err := m.FilePath("../a-1.0.0.bin"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if _, err := m.FilePath("a-1.0.0.bin"); err != nil {
		t.Fatal(err)
	}
}
