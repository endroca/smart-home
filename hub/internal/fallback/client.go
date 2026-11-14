// Package fallback implements the LAN HTTP client the hub uses to reach the
// devices. It talks to the firmware's /api endpoints with the per-device
// bearer token.
package fallback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"smart_home/hub/internal/model"
	"smart_home/hub/internal/store"
)

var (
	ErrNoURL   = errors.New("device URL not known")
	ErrNoToken = errors.New("device token not configured")
)

// Directory resolves the per-thing LAN fallback credentials. It is implemented
// by the hub database.
type Directory interface {
	DeviceToken(thing string) string
	DeviceURL(thing string) string
}

// HTTPError is an error returned by the device API.
type HTTPError struct {
	Status  int
	Code    string
	Message string
}

func (e *HTTPError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("device HTTP %d: %s (%s)", e.Status, e.Message, e.Code)
	}
	return fmt.Sprintf("device HTTP %d: %s", e.Status, e.Message)
}

// Client talks to the ESP32 LAN API.
type Client struct {
	http  *http.Client
	store *store.Store
	dir   Directory

	// mdns caches the IP behind a *.local name; Go's resolver is slow for mDNS
	// (seconds per lookup) so we resolve once and reuse.
	mu   sync.Mutex
	mdns map[string]mdnsEntry
}

type mdnsEntry struct {
	ip string
	at time.Time
	ok bool
}

// New returns a fallback client.
func New(st *store.Store, dir Directory) *Client {
	return &Client{
		http:  &http.Client{Timeout: 30 * time.Second},
		store: st,
		dir:   dir,
		mdns:  map[string]mdnsEntry{},
	}
}

// BaseURL returns the best known URL for a thing. A concrete LAN IP is always
// preferred: a *.local name is slow to resolve (Go's resolver does not do mDNS)
// and a configured *.local URL is therefore only used when no IP is known.
func (c *Client) BaseURL(thing string) (string, error) {
	configured := ""
	if c.dir != nil {
		configured = c.dir.DeviceURL(thing)
	}
	if configured != "" && !strings.HasSuffix(urlHost(configured), ".local") {
		// An explicit, non-mDNS override wins.
		return normalizeURL(configured), nil
	}

	statusIP := ""
	manifestMDNS := ""
	manifestPort := 0
	if t, ok := c.store.Get(thing); ok {
		if t.Status != nil {
			statusIP = t.Status.IP
		}
		if t.Manifest != nil {
			manifestMDNS = t.Manifest.HTTP.MDNS
			manifestPort = t.Manifest.HTTP.Port
		}
	}
	if statusIP != "" {
		return fmt.Sprintf("http://%s:80", statusIP), nil
	}
	// A gateway that announced itself over mDNS is reachable without any
	// configuration; prefer that to the mDNS name, which the Go resolver cannot
	// look up on its own.
	if announced := c.store.AnnouncedURL(thing); announced != "" {
		return normalizeURL(announced), nil
	}
	if configured != "" {
		return c.resolveURL(configured), nil
	}
	if manifestMDNS != "" {
		if manifestPort <= 0 {
			manifestPort = 80
		}
		return c.resolveURL(fmt.Sprintf("http://%s:%d", manifestMDNS, manifestPort)), nil
	}
	return c.resolveURL(fmt.Sprintf("http://%s:80", thing+".local")), nil
}

func normalizeURL(u string) string {
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "http://" + u
	}
	return strings.TrimRight(u, "/")
}

func urlHost(u string) string {
	u = normalizeURL(u)
	if parsed, err := url.Parse(u); err == nil {
		return parsed.Hostname()
	}
	return ""
}

// resolveURL rewrites a *.local host to a cached IP so requests are fast.
func (c *Client) resolveURL(u string) string {
	u = normalizeURL(u)
	parsed, err := url.Parse(u)
	if err != nil || parsed.Hostname() == "" {
		return u
	}
	host := parsed.Hostname()
	if !strings.HasSuffix(host, ".local") {
		return u
	}
	ip := c.resolveMDNS(host)
	if ip == "" || ip == host {
		return u
	}
	if port := parsed.Port(); port != "" {
		parsed.Host = net.JoinHostPort(ip, port)
	} else {
		parsed.Host = ip
	}
	return parsed.String()
}

func (c *Client) resolveMDNS(host string) string {
	c.mu.Lock()
	if e, ok := c.mdns[host]; ok {
		ttl := 5 * time.Minute
		if !e.ok {
			// Negative-cache a failure for a short window so a probe does not
			// block for the full lookup timeout on every retry.
			ttl = 30 * time.Second
		}
		if time.Since(e.at) < ttl {
			c.mu.Unlock()
			return e.ip
		}
	}
	c.mu.Unlock()

	// The lookup can block, and a gateway that only has a *.local name is the
	// common case right after registration: keep the wait short so a probe is
	// not stalled for seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil || len(ips) == 0 {
		c.mu.Lock()
		c.mdns[host] = mdnsEntry{ip: host, at: time.Now()}
		c.mu.Unlock()
		return host
	}
	ip := ips[0]
	c.mu.Lock()
	c.mdns[host] = mdnsEntry{ip: ip, at: time.Now(), ok: true}
	c.mu.Unlock()
	return ip
}

// FetchManifest reads the device discovery document over the LAN
// (GET /api/manifest), so the hub discovers any device the same way, regardless
// of what it exposes.
func (c *Client) FetchManifest(ctx context.Context, thing string) (*model.Manifest, error) {
	var m model.Manifest
	if err := c.request(ctx, thing, http.MethodGet, "/api/manifest", nil, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Diag fetches the device's local diagnostics (GET /api/diag): RF radio/counters,
// NVS, heap, uptime, hub link and pendingSync. It is a LAN-only view.
func (c *Client) Diag(ctx context.Context, thing string) (map[string]any, error) {
	var out map[string]any
	if err := c.request(ctx, thing, http.MethodGet, "/api/diag", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetHubURL tells the device which URL it should push state to (POST
// /api/hub). The device persists it and immediately queues its state/status.
func (c *Client) SetHubURL(ctx context.Context, thing, url string) (map[string]any, error) {
	var out map[string]any
	if err := c.request(ctx, thing, http.MethodPost, "/api/hub", map[string]any{"url": url}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Available reports whether a token and a **reachable** address are known. A
// bare *.local name that does not resolve is not reachable here: the Go
// resolver does not do mDNS, so the hub would only fail later, on the request.
func (c *Client) Available(thing string) bool {
	base, err := c.BaseURL(thing)
	if err != nil {
		return false
	}
	return dialable(base)
}

func (c *Client) token(thing string) (string, error) {
	if c.dir == nil {
		return "", ErrNoToken
	}
	t := c.dir.DeviceToken(thing)
	if t == "" {
		return "", ErrNoToken
	}
	return t, nil
}

func (c *Client) request(ctx context.Context, thing, method, path string, body any, out any) error {
	base, err := c.BaseURL(thing)
	if err != nil {
		return err
	}
	token, err := c.token(thing)
	if err != nil {
		return err
	}
	if !dialable(base) {
		// The host is a *.local name that could not be resolved: fail fast
		// instead of letting the HTTP client block on an unsupported mDNS
		// lookup, and tell the operator what to do about it.
		return fmt.Errorf("cannot reach %s: no LAN address is known and %s does not resolve here. Set the gateway URL (or IP) in the hub", thing, urlHost(base))
	}
	err = c.doRequest(ctx, base, token, method, path, body, out)
	if err == nil {
		return nil
	}
	if _, ok := err.(*HTTPError); ok {
		// The device answered (even with an error): do not retry.
		return err
	}
	// Transport failure: the address may be stale (DHCP change). Retry once via
	// a freshly resolved mDNS name.
	if alt := c.mdnsBaseURL(thing); alt != "" && alt != base && dialable(alt) {
		if err2 := c.doRequest(ctx, alt, token, method, path, body, out); err2 == nil {
			return nil
		}
	}
	return err
}

// dialable reports whether a base URL has a concrete, resolvable host. A
// *.local name that could not be resolved would make the HTTP client block on
// the (unsupported) mDNS lookup, so we skip it instead.
func dialable(base string) bool {
	host := urlHost(base)
	return host != "" && !strings.HasSuffix(host, ".local")
}

func (c *Client) doRequest(ctx context.Context, base, token, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpError(resp.StatusCode, data)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(out)
}

// mdnsBaseURL returns the device URL resolved fresh through its mDNS name, used
// as a retry when the known IP failed (for example after a DHCP change). It
// invalidates the cached address so a new IP is picked up.
func (c *Client) mdnsBaseURL(thing string) string {
	host := thing + ".local"
	if t, ok := c.store.Get(thing); ok && t.Manifest != nil && t.Manifest.HTTP.MDNS != "" {
		host = t.Manifest.HTTP.MDNS
	}
	c.mu.Lock()
	delete(c.mdns, host)
	c.mu.Unlock()
	return c.resolveURL(fmt.Sprintf("http://%s:80", host))
}

func httpError(status int, data []byte) error {
	var wrapper struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &wrapper)
	code := wrapper.Error.Code
	msg := wrapper.Error.Message
	if code == "" && msg == "" {
		msg = strings.TrimSpace(string(data))
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &HTTPError{Status: status, Code: code, Message: msg}
}

// ApplyState applies an absolute state over LAN and returns the refreshed state.
func (c *Client) ApplyState(ctx context.Context, thing, deviceID string, state map[string]any, force bool) (map[string]any, *model.DeviceState, error) {
	var resp struct {
		State       map[string]any `json:"state"`
		Transmitted []string       `json:"transmitted"`
	}
	body := map[string]any{
		"id":    deviceID,
		"state": state,
	}
	if force {
		body["force"] = true
	}
	if err := c.request(ctx, thing, http.MethodPost, "/api/state", body, &resp); err != nil {
		return nil, nil, err
	}
	sh, err := c.FetchState(ctx, thing, deviceID)
	if err != nil {
		return map[string]any{"state": resp.State, "transmitted": resp.Transmitted}, nil, nil
	}
	return map[string]any{"state": resp.State, "transmitted": resp.Transmitted}, sh, nil
}

// Correct applies a state correction without RF.
func (c *Client) Correct(ctx context.Context, thing, deviceID string, state map[string]any) (map[string]any, *model.DeviceState, error) {
	var resp map[string]any
	if err := c.request(ctx, thing, http.MethodPost, "/api/correct", map[string]any{
		"id":    deviceID,
		"state": state,
	}, &resp); err != nil {
		return nil, nil, err
	}
	sh, err := c.FetchState(ctx, thing, deviceID)
	if err != nil {
		return resp, nil, nil
	}
	return resp, sh, nil
}

// FetchState reads /api/state and extracts the reported state of one sub-device.
func (c *Client) FetchState(ctx context.Context, thing, deviceID string) (*model.DeviceState, error) {
	var resp struct {
		Fans []struct {
			DeviceID  string         `json:"id"`
			Name      string         `json:"name"`
			State     map[string]any `json:"state"`
			Source    string         `json:"source"`
			UpdatedAt any            `json:"updatedAt"`
			Counter   any            `json:"counter"`
		} `json:"fans"`
	}
	if err := c.request(ctx, thing, http.MethodGet, "/api/state", nil, &resp); err != nil {
		return nil, err
	}
	for _, fan := range resp.Fans {
		if fan.DeviceID != deviceID {
			continue
		}
		reported := map[string]any{}
		for k, v := range fan.State {
			reported[k] = v
		}
		if fan.Source != "" {
			reported["source"] = fan.Source
		}
		if fan.UpdatedAt != nil {
			reported["updatedAt"] = fan.UpdatedAt
		}
		if fan.Counter != nil {
			reported["counter"] = fan.Counter
		}
		return &model.DeviceState{Thing: thing, DeviceID: deviceID, Reported: reported, UpdatedAt: time.Now().UTC()}, nil
	}
	return nil, fmt.Errorf("device %s not present in /api/state", deviceID)
}

// Command executes a firmware command over HTTP.
func (c *Client) Command(ctx context.Context, thing, action string, fields map[string]any) (map[string]any, error) {
	var method, path string
	body := map[string]any{}
	switch action {
	case "fan.add":
		method, path = http.MethodPost, "/api/fan/add"
	case "fan.remove":
		method, path = http.MethodPost, "/api/fan/remove"
		body["id"] = fields["id"]
	case "config.get":
		method, path = http.MethodGet, "/api/config"
	case "config.set":
		method, path = http.MethodPost, "/api/config"
		body["config"] = fields["config"]
	case "reboot":
		method, path = http.MethodPost, "/api/reboot"
	case "learn.start":
		method, path = http.MethodPost, "/api/learn/start"
		body["id"] = fields["id"]
		body["command"] = fields["command"]
	case "learn.cancel":
		method, path = http.MethodPost, "/api/learn/cancel"
	case "state.correct":
		method, path = http.MethodPost, "/api/correct"
		body["id"] = fields["id"]
		body["state"] = fields["state"]
	default:
		return nil, fmt.Errorf("fallback action %q is not supported", action)
	}
	if method == http.MethodGet {
		body = nil
	}
	var out map[string]any
	if err := c.request(ctx, thing, method, path, body, &out); err != nil {
		return nil, err
	}
	if ok, has := model.AsBool(out["ok"]); has && !ok {
		return nil, deviceError(out)
	}
	return out, nil
}

func deviceError(out map[string]any) error {
	raw, _ := out["error"].(map[string]any)
	code, _ := model.AsString(raw["code"])
	message, _ := model.AsString(raw["message"])
	if message == "" {
		message = code
	}
	if message == "" {
		message = "device rejected the command"
	}
	return &HTTPError{Status: http.StatusOK, Code: code, Message: message}
}

// Learn starts a learning session over HTTP and polls until it finishes.
func (c *Client) Learn(ctx context.Context, thing, deviceID, command string) (map[string]any, error) {
	if _, err := c.Command(ctx, thing, "learn.start", map[string]any{
		"id": deviceID, "command": command,
	}); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		var status map[string]any
		if err := c.request(ctx, thing, http.MethodGet, "/api/learn/status", nil, &status); err != nil {
			return nil, err
		}
		if result, _ := status["result"].(string); result != "listening" && result != "idle" && result != "" {
			return status, nil
		}
	}
	return nil, errors.New("learn status timeout")
}

// UploadFirmware uploads a .bin to the device's local OTA endpoint.
func (c *Client) UploadFirmware(ctx context.Context, thing, path, sha256 string) (map[string]any, error) {
	base, err := c.BaseURL(thing)
	if err != nil {
		return nil, err
	}
	token, err := c.token(thing)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("firmware", filepath.Base(path))
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	u := base + "/api/update?sha256=" + url.QueryEscape(sha256)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respData, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpError(resp.StatusCode, respData)
	}
	var out map[string]any
	if len(respData) > 0 {
		dec := json.NewDecoder(bytes.NewReader(respData))
		dec.UseNumber()
		_ = dec.Decode(&out)
	}
	if out == nil {
		out = map[string]any{"ok": true}
	}
	return out, nil
}
