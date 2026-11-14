package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"smart_home/hub/internal/config"
	"smart_home/hub/internal/hub"
)

func firmwareServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{HTTPAddr: ":0", FirmwareDir: dir}
	h, err := hub.New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(h, cfg, nil).Handler())
	t.Cleanup(srv.Close)
	return srv, dir
}

func uploadRequest(t *testing.T, url, filename, body string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url+"/api/firmware", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestFirmwareUploadAndDelete(t *testing.T) {
	srv, dir := firmwareServer(t)

	resp, err := http.DefaultClient.Do(uploadRequest(t, srv.URL, "smart_control-1.2.3.bin", "firmware-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload status=%d body=%s", resp.StatusCode, data)
	}
	var got struct {
		OK    bool `json:"ok"`
		Files []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Size    int64  `json:"size"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || len(got.Files) != 1 || got.Files[0].Name != "smart_control-1.2.3.bin" {
		t.Fatalf("upload payload=%+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "smart_control-1.2.3.bin")); err != nil {
		t.Fatalf("file not stored: %v", err)
	}

	// Lista tambem deve ve-lo.
	lresp, err := http.Get(srv.URL + "/api/firmware")
	if err != nil {
		t.Fatal(err)
	}
	defer lresp.Body.Close()
	if lresp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d", lresp.StatusCode)
	}

	// Nome invalido (extensao errada) e rejeitado sem gravar. O parser
	// multipart ja tira o diretorio do filename, entao testamos a extensao.
	bad, err := http.DefaultClient.Do(uploadRequest(t, srv.URL, "notafirmware.txt", "x"))
	if err != nil {
		t.Fatal(err)
	}
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad upload status=%d", bad.StatusCode)
	}

	// DELETE remove o arquivo.
	dreq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/firmware/smart_control-1.2.3.bin", nil)
	dresp, err := http.DefaultClient.Do(dreq)
	if err != nil {
		t.Fatal(err)
	}
	dresp.Body.Close()
	if dresp.StatusCode != http.StatusOK {
		t.Fatalf("delete status=%d", dresp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(dir, "smart_control-1.2.3.bin")); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
}
