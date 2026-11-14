// Package ota serves firmware files and coordinates over-the-air updates: the
// hub uploads a .bin to the device's LAN endpoint, which flashes it and
// reboots.
package ota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"smart_home/hub/internal/config"
	"smart_home/hub/internal/model"
	"smart_home/hub/internal/store"
)

// Uploader performs the local HTTP OTA upload.
type Uploader interface {
	UploadFirmware(ctx context.Context, thing, path, sha256 string) (map[string]any, error)
}

// File is one available .bin in the firmware directory.
type File struct {
	Name    string    `json:"name"`
	Version string    `json:"version"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// Pending is an in-flight update.
type Pending struct {
	Thing    string    `json:"thing"`
	File     string    `json:"file"`
	Version  string    `json:"version"`
	Previous string    `json:"previous,omitempty"`
	Rollback bool      `json:"rollback,omitempty"`
	Deadline time.Time `json:"deadline"`
}

// Manager owns the firmware directory and pending installs.
type Manager struct {
	dir      string
	store    *store.Store
	uploader Uploader

	mu       sync.Mutex
	pending  map[string]*Pending
	lastGood map[string]string
}

var versionRE = regexp.MustCompile(`([0-9]+(?:\.[0-9]+){1,3})`)

// nome aceito para um firmware enviado pela UI.
var nameRE = regexp.MustCompile(`^[A-Za-z0-9._-]+\.bin$`)

// maxFirmwareBytes limita um upload (o .bin tem ~1,5 MB hoje).
const maxFirmwareBytes = 8 << 20

// ErrInvalidName e devolvido quando o nome do arquivo nao e aceito.
var ErrInvalidName = errors.New("invalid firmware name")

// Save grava um .bin enviado pela UI no diretorio de firmware, de forma
// atomica (arquivo temporario + rename). O nome precisa casar com nameRE e o
// corpo nao pode passar de maxFirmwareBytes.
func (m *Manager) Save(name string, r io.Reader) ([]File, error) {
	// nameRE ja exclui separadores de caminho, entao "../x.bin" e rejeitado
	// (nao ha traversal silencioso via filepath.Base).
	name = strings.TrimSpace(name)
	if !nameRE.MatchString(name) {
		return nil, ErrInvalidName
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(m.dir, ".upload-*.bin")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	// Ler um byte a mais detecta corpo maior que o limite.
	n, copyErr := io.Copy(tmp, io.LimitReader(r, maxFirmwareBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if n == 0 {
		return nil, errors.New("empty firmware")
	}
	if n > maxFirmwareBytes {
		return nil, errors.New("firmware too large")
	}
	if err := os.Rename(tmpName, filepath.Join(m.dir, name)); err != nil {
		return nil, err
	}
	log.Printf("[ota] firmware %s armazenado (%d bytes)", name, n)
	return m.List()
}

// Delete remove um .bin do diretorio de firmware.
func (m *Manager) Delete(name string) ([]File, error) {
	name = strings.TrimSpace(name)
	if !nameRE.MatchString(name) {
		return nil, ErrInvalidName
	}
	if err := os.Remove(filepath.Join(m.dir, name)); err != nil {
		return nil, err
	}
	return m.List()
}

// New creates an OTA manager.
func New(cfg config.Config, st *store.Store, uploader Uploader) *Manager {
	return &Manager{
		dir:      cfg.FirmwareDir,
		store:    st,
		uploader: uploader,
		pending:  map[string]*Pending{},
		lastGood: map[string]string{},
	}
}

// Dir returns the configured firmware directory.
func (m *Manager) Dir() string { return m.dir }

// List returns .bin files sorted newest first.
func (m *Manager) List() ([]File, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []File{}, nil
		}
		return nil, err
	}
	out := make([]File, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".bin") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out = append(out, File{
			Name:    entry.Name(),
			Version: versionFromFile(entry.Name()),
			Size:    info.Size(),
			ModTime: info.ModTime().UTC(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].Name > out[j].Name
		}
		return out[i].ModTime.After(out[j].ModTime)
	})
	return out, nil
}

func versionFromFile(name string) string {
	if m := versionRE.FindStringSubmatch(name); len(m) > 1 {
		return m[1]
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	return base
}

// FindFile returns the file whose version matches version.
func (m *Manager) FindFile(version string) (string, bool) {
	if version == "" {
		return "", false
	}
	files, err := m.List()
	if err != nil {
		return "", false
	}
	for _, f := range files {
		if f.Version == version {
			return f.Name, true
		}
	}
	return "", false
}

// Install starts an OTA update for a thing.
func (m *Manager) Install(ctx context.Context, thing, name string) (*Pending, error) {
	return m.install(ctx, thing, name, false)
}

func (m *Manager) install(ctx context.Context, thing, name string, rollback bool) (*Pending, error) {
	if thing == "" {
		return nil, errors.New("thing is required")
	}
	if filepath.Base(name) != name || !strings.HasSuffix(strings.ToLower(name), ".bin") {
		return nil, errors.New("invalid firmware file")
	}
	path := filepath.Join(m.dir, name)
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, errors.New("firmware path is a directory")
	}

	version := versionFromFile(name)
	if t, ok := m.store.Get(thing); ok && t.Status != nil && t.Status.Version == version {
		return nil, errors.New("device already reports version " + version)
	}

	m.mu.Lock()
	if _, exists := m.pending[thing]; exists {
		m.mu.Unlock()
		return nil, errors.New("ota already in progress for this thing")
	}
	previous := m.lastGood[thing]
	if previous == "" {
		if t, ok := m.store.Get(thing); ok && t.Status != nil {
			if f, ok := m.FindFile(t.Status.Version); ok {
				previous = f
			}
		}
	}
	p := &Pending{
		Thing:    thing,
		File:     name,
		Version:  version,
		Previous: previous,
		Rollback: rollback,
		Deadline: time.Now().Add(10 * time.Minute),
	}
	m.pending[thing] = p
	m.mu.Unlock()

	fail := func(err error) (*Pending, error) {
		m.mu.Lock()
		delete(m.pending, thing)
		m.mu.Unlock()
		return nil, err
	}

	sha, err := fileSHA256(path)
	if err != nil {
		return fail(err)
	}
	if m.uploader == nil {
		return fail(errors.New("no LAN uploader configured for this thing"))
	}
	if _, err := m.uploader.UploadFirmware(ctx, thing, path, sha); err != nil {
		return fail(err)
	}

	m.mu.Lock()
	m.pending[thing] = p
	m.mu.Unlock()
	log.Printf("[ota] %s -> %s (%s)", thing, name, version)
	return p, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// OnVersion observes a status/manifest firmware version and marks pending
// installs as done.
func (m *Manager) OnVersion(thing, version string) {
	if version == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pending[thing]
	if p == nil || p.Version != version {
		return
	}
	if p.Rollback {
		log.Printf("[ota] rollback confirmed for %s (%s)", thing, version)
	} else {
		m.lastGood[thing] = p.File
		log.Printf("[ota] confirmed %s on %s", version, thing)
	}
	delete(m.pending, thing)
}

// OnEvent handles ota.done/progress from the device.
func (m *Manager) OnEvent(thing string, event map[string]any) {
	name, _ := event["event"].(string)
	if name != "ota.done" {
		return
	}
	ok, _ := model.AsBool(event["ok"])
	m.mu.Lock()
	p := m.pending[thing]
	if p == nil {
		m.mu.Unlock()
		return
	}
	if ok {
		// Keep pending until status/manifest confirms the running version.
		m.mu.Unlock()
		return
	}
	delete(m.pending, thing)
	m.mu.Unlock()
	log.Printf("[ota] device reported failure for %s: %v", thing, event["error"])
}

// Loop checks pending deadlines and rolls back when the new version never
// becomes visible.
func (m *Manager) Loop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkDeadlines(ctx)
		}
	}
}

func (m *Manager) checkDeadlines(ctx context.Context) {
	m.mu.Lock()
	type expired struct {
		thing string
		p     Pending
	}
	var list []expired
	for thing, p := range m.pending {
		if time.Now().Before(p.Deadline) {
			continue
		}
		list = append(list, expired{thing: thing, p: *p})
		delete(m.pending, thing)
	}
	m.mu.Unlock()

	for _, item := range list {
		p := item.p
		if t, ok := m.store.Get(item.thing); ok && t.Status != nil && t.Status.Version == p.Version {
			if !p.Rollback {
				m.mu.Lock()
				m.lastGood[item.thing] = p.File
				m.mu.Unlock()
			}
			log.Printf("[ota] deadline observed confirmed version %s on %s", p.Version, item.thing)
			continue
		}
		if p.Rollback {
			log.Printf("[ota] rollback also timed out for %s, manual intervention needed", item.thing)
			continue
		}
		if p.Previous == "" {
			log.Printf("[ota] update %s on %s timed out and no previous binary is known", p.File, item.thing)
			continue
		}
		log.Printf("[ota] update %s on %s timed out; rolling back to %s", p.File, item.thing, p.Previous)
		if _, err := m.install(ctx, item.thing, p.Previous, true); err != nil {
			log.Printf("[ota] rollback for %s failed: %v", item.thing, err)
		}
	}
}

// Pending returns a copy of all pending installs.
func (m *Manager) Pending() map[string]Pending {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]Pending, len(m.pending))
	for k, v := range m.pending {
		out[k] = *v
	}
	return out
}

// FilePath returns the safe path for a served firmware file.
func (m *Manager) FilePath(name string) (string, error) {
	if filepath.Base(name) != name || !strings.HasSuffix(strings.ToLower(name), ".bin") {
		return "", errors.New("invalid firmware file")
	}
	path := filepath.Join(m.dir, name)
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", errors.New("firmware path is a directory")
	}
	return path, nil
}

// VersionFromFile is exported for tests and UI helpers.
func VersionFromFile(name string) string { return versionFromFile(name) }
