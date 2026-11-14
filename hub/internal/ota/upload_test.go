package ota

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"smart_home/hub/internal/config"
	"smart_home/hub/internal/store"
)

func newUploadManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	return New(config.Config{FirmwareDir: dir}, store.New(), nil), dir
}

func TestSaveStoresFirmware(t *testing.T) {
	m, dir := newUploadManager(t)

	files, err := m.Save("smart_control-1.2.3.bin", strings.NewReader("firmware-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "smart_control-1.2.3.bin" {
		t.Fatalf("files=%+v", files)
	}
	if files[0].Version != "1.2.3" {
		t.Fatalf("version=%q", files[0].Version)
	}

	data, err := os.ReadFile(filepath.Join(dir, "smart_control-1.2.3.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "firmware-bytes" {
		t.Fatalf("content=%q", data)
	}

	// Nao deixa arquivo temporario para tras.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftover temp files: %d entries", len(entries))
	}
}

func TestSaveOverwrites(t *testing.T) {
	m, _ := newUploadManager(t)
	if _, err := m.Save("fw-1.0.0.bin", strings.NewReader("v1")); err != nil {
		t.Fatal(err)
	}
	files, err := m.Save("fw-1.0.0.bin", strings.NewReader("v2-longer"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Size != int64(len("v2-longer")) {
		t.Fatalf("files=%+v", files)
	}
}

func TestSaveRejectsBadInput(t *testing.T) {
	m, _ := newUploadManager(t)

	cases := []struct {
		name string
		body string
	}{
		{"../evil.bin", "x"},
		{"a/b.bin", "x"},
		{"firmware.txt", "x"},
		{"noext", "x"},
		{"fw 1.bin", "x"},
	}
	for _, c := range cases {
		if _, err := m.Save(c.name, strings.NewReader(c.body)); err == nil {
			t.Fatalf("%q: expected rejection", c.name)
		}
	}

	if _, err := m.Save("empty-1.0.0.bin", strings.NewReader("")); err == nil {
		t.Fatal("empty body accepted")
	}
}

func TestSaveRejectsTooLarge(t *testing.T) {
	m, _ := newUploadManager(t)
	big := strings.NewReader(strings.Repeat("x", maxFirmwareBytes+1))
	if _, err := m.Save("big-1.0.0.bin", big); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected too-large error, got %v", err)
	}
}

func TestDeleteFirmware(t *testing.T) {
	m, dir := newUploadManager(t)
	if _, err := m.Save("fw-1.0.0.bin", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	files, err := m.Delete("fw-1.0.0.bin")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("files=%+v", files)
	}
	if _, err := os.Stat(filepath.Join(dir, "fw-1.0.0.bin")); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
	if _, err := m.Delete("../x.bin"); err == nil {
		t.Fatal("delete accepted traversal name")
	}
}
