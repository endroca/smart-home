package tuya

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// This file implements the small slice of the Tuya IoT Cloud API needed to
// register a device locally: fetch the account's device list (which carries the
// per-device local_key) and, optionally, the DP specifications. The local
// protocol in the rest of this package never talks to the cloud; the cloud is
// only used once to obtain the keys.

// cloudHosts maps a Tuya data center code to its API host.
var cloudHosts = map[string]string{
	"cn":   "openapi.tuyacn.com",
	"ay":   "openapi.tuyacn.com",
	"us":   "openapi.tuyaus.com",
	"az":   "openapi.tuyaus.com",
	"us-e": "openapi-ueaz.tuyaus.com",
	"ue":   "openapi-ueaz.tuyaus.com",
	"eu":   "openapi.tuyaeu.com",
	"eu-w": "openapi-weaz.tuyaeu.com",
	"we":   "openapi-weaz.tuyaeu.com",
	"in":   "openapi.tuyain.com",
	"sg":   "openapi-sg.iotbing.com",
}

// CloudConfig holds the Tuya IoT Platform credentials.
//
// AccessID/AccessSecret are the "Access ID/Client ID" and "Access Secret/Client
// Secret" from the project's Authorization Key page. Region is the data center
// code (us, eu, cn, in, sg, us-e or eu-w).
type CloudConfig struct {
	AccessID     string
	AccessSecret string
	Region       string

	// BaseURL overrides the region-derived host, e.g. for tests.
	BaseURL string
	// HTTPClient overrides the default client (10s timeout).
	HTTPClient *http.Client
	// Timeout sets the default client timeout when HTTPClient is nil.
	Timeout time.Duration
}

// CloudDevice is one device as reported by the Tuya Cloud.
type CloudDevice struct {
	ID        string
	Name      string
	LocalKey  string
	UID       string
	ProductID string
	Category  string
	IP        string
	Version   string
	Online    bool
	Raw       map[string]any
}

// DPSpec describes one data point from the device specification.
type DPSpec struct {
	DPID     string
	Code     string
	Name     string
	Desc     string
	Type     string
	Values   any
	Writable bool
}

// ErrNotSubscribed reports that the Tuya project does not have the API
// subscription (or trial permission package) required by a feature, such as the
// Power Management energy statistics.
var ErrNotSubscribed = errors.New("tuya: this API is not subscribed for the project")

// Cloud is a minimal Tuya IoT Cloud client.
type Cloud struct {
	cfg    CloudConfig
	client *http.Client
	base   string

	mu         sync.Mutex
	token      string
	tokenUntil time.Time
	offset     time.Duration
}

// NewCloud validates the credentials and prepares the API host.
func NewCloud(cfg CloudConfig) (*Cloud, error) {
	if strings.TrimSpace(cfg.AccessID) == "" || strings.TrimSpace(cfg.AccessSecret) == "" {
		return nil, errors.New("tuya: cloud access id and access secret are required")
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		region := strings.ToLower(strings.TrimSpace(cfg.Region))
		host, ok := cloudHosts[region]
		if !ok {
			return nil, fmt.Errorf("tuya: unknown cloud region %q (use us, eu, cn, in, sg, us-e or eu-w)", cfg.Region)
		}
		base = "https://" + host
	}
	client := cfg.HTTPClient
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	return &Cloud{cfg: cfg, client: client, base: base}, nil
}

// Token returns a valid access token, requesting a new one when needed.
func (c *Cloud) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.token != "" && time.Now().Before(c.tokenUntil) {
		tok := c.token
		c.mu.Unlock()
		return tok, nil
	}
	c.mu.Unlock()

	res, err := c.do(ctx, http.MethodGet, "/v1.0/token", map[string]string{"grant_type": "1"}, nil, false)
	if err != nil {
		return "", err
	}
	result, _ := res["result"].(map[string]any)
	tok, _ := result["access_token"].(string)
	if tok == "" {
		return "", errors.New("tuya: cloud token missing from response")
	}
	expire := 3600.0
	if v, ok := result["expire_time"].(float64); ok && v > 0 {
		expire = v
	}
	if v, ok := res["t"].(float64); ok && v > 0 {
		// Correct for local clock skew using the server timestamp.
		c.mu.Lock()
		c.offset = time.Duration(v*float64(time.Millisecond)) - time.Duration(time.Now().UnixMilli())*time.Millisecond
		c.mu.Unlock()
	}
	c.mu.Lock()
	c.token = tok
	c.tokenUntil = time.Now().Add(time.Duration(expire)*time.Second - time.Minute)
	c.mu.Unlock()
	return tok, nil
}

// Devices returns every device on the account. seedID is any device ID already
// registered in the app; it is only used to discover the owning user ID.
func (c *Cloud) Devices(ctx context.Context, seedID string) ([]CloudDevice, error) {
	uid, err := c.userID(ctx, seedID)
	if err != nil {
		return nil, err
	}
	res, err := c.do(ctx, http.MethodGet, "/v1.0/users/"+url.PathEscape(uid)+"/devices", nil, nil, true)
	if err != nil {
		return nil, err
	}
	items, ok := res["result"].([]any)
	if !ok {
		return nil, errors.New("tuya: cloud device list is not a list")
	}
	out := make([]CloudDevice, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, cloudDeviceFrom(m))
		}
	}
	return out, nil
}

// Device returns a single device by ID.
func (c *Cloud) Device(ctx context.Context, id string) (CloudDevice, error) {
	res, err := c.do(ctx, http.MethodGet, "/v1.0/devices/"+url.PathEscape(id), nil, nil, true)
	if err != nil {
		return CloudDevice{}, err
	}
	result, _ := res["result"].(map[string]any)
	if result == nil {
		return CloudDevice{}, fmt.Errorf("tuya: cloud device %s not found", id)
	}
	return cloudDeviceFrom(result), nil
}

// userID resolves the account user ID that owns the given device.
func (c *Cloud) userID(ctx context.Context, id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", errors.New("tuya: a seed device ID is required to list cloud devices")
	}
	res, err := c.do(ctx, http.MethodGet, "/v1.0/devices/"+url.PathEscape(id), nil, nil, true)
	if err != nil {
		return "", err
	}
	result, _ := res["result"].(map[string]any)
	uid, _ := result["uid"].(string)
	if uid == "" {
		return "", fmt.Errorf("tuya: cloud device %s has no uid (is it linked to the app account?)", id)
	}
	return uid, nil
}

// Specifications returns the DP functions (writable) and status codes for a
// device.
func (c *Cloud) Specifications(ctx context.Context, id string) ([]DPSpec, error) {
	res, err := c.do(ctx, http.MethodGet, "/v1.1/devices/"+url.PathEscape(id)+"/specifications", nil, nil, true)
	if err != nil {
		return nil, err
	}
	result, _ := res["result"].(map[string]any)
	out := []DPSpec{}
	for _, key := range []string{"functions", "status"} {
		items, _ := result[key].([]any)
		for _, it := range items {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			spec := DPSpec{Code: stringField(m, "code"), Name: stringField(m, "name"), Desc: stringField(m, "desc"), Type: stringField(m, "type"), Writable: key == "functions"}
			if dp := stringField(m, "dp_id"); dp != "" {
				spec.DPID = dp
			} else {
				spec.DPID = spec.Code
			}
			if raw, ok := m["values"].(string); ok {
				var decoded any
				if json.Unmarshal([]byte(raw), &decoded) == nil {
					spec.Values = decoded
				} else {
					spec.Values = raw
				}
			}
			out = append(out, spec)
		}
	}
	return out, nil
}

// Command sends one DP value to a device through the cloud API. code is the DP
// function code (the cloud uses names, not ids).
func (c *Cloud) Command(ctx context.Context, id, code string, value any) error {
	body := map[string]any{
		"commands": []any{map[string]any{"code": code, "value": value}},
	}
	_, err := c.do(ctx, http.MethodPost, "/v1.0/devices/"+url.PathEscape(id)+"/commands", nil, body, true)
	return err
}

// DPValue is one current data point returned by the cloud (keyed by code).
type DPValue struct {
	Code  string
	Value any
}

// Status returns the current data-point values of a device.
func (c *Cloud) Status(ctx context.Context, id string) ([]DPValue, error) {
	res, err := c.do(ctx, http.MethodGet, "/v1.0/devices/"+url.PathEscape(id)+"/status", nil, nil, true)
	if err != nil {
		return nil, err
	}
	items, _ := res["result"].([]any)
	out := make([]DPValue, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, DPValue{Code: stringField(m, "code"), Value: m["value"]})
	}
	return out, nil
}

// EnergyPoint is one bucket of the cloud energy-consumption trend.
type EnergyPoint struct {
	Time  string
	Value float64
}

// EnergyTrend fetches the electricity-consumption trend of a device from the
// Power Management API.
//
// statisticsType is "hour" or "day"; start and end use the matching format
// (yyyyMMddHH for hour, yyyyMMdd for day). The response is a list of buckets
// whose value is the consumed energy in kWh.
func (c *Cloud) EnergyTrend(ctx context.Context, deviceID, statisticsType, start, end string) ([]EnergyPoint, error) {
	query := map[string]string{
		"deviceId":       deviceID,
		"energyAction":   "consume",
		"statisticsType": statisticsType,
		"startTime":      start,
		"endTime":        end,
	}
	res, err := c.do(ctx, http.MethodGet, "/v1.0/iot-03/energy/electricity/devices/statistics-trend", query, nil, true)
	if err != nil {
		return nil, err
	}
	items, ok := res["result"].([]any)
	if !ok {
		return nil, errors.New("tuya: energy trend is not a list")
	}
	out := make([]EnergyPoint, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, EnergyPoint{Time: stringField(m, "time"), Value: floatField(m, "value")})
	}
	return out, nil
}

// EnergyLog is one entry of the device status report log. EventTime is the
// device timestamp in Unix milliseconds and Value is the raw data-point value
// (for add_ele, a delta in units of 10^-scale kWh).
type EnergyLog struct {
	Code      string
	EventTime int64
	Value     float64
}

// energyLogPageSize is the report-logs page size, and energyLogMaxPages bounds
// pagination so a device that reports every minute cannot hang the request.
const (
	energyLogPageSize = 100
	energyLogMaxPages = 60
)

// EnergyLogs fetches device status report logs for the given DP codes between
// start and end (both Unix milliseconds).
//
// This is the source of the energy history: add_ele is reported periodically
// as an increment, even when the device is not bound to a Power Management
// space (statistics-trend returns zeros in that case). The response is newest
// first; callers that need chronological order must sort it.
func (c *Cloud) EnergyLogs(ctx context.Context, deviceID string, codes []string, start, end int64) ([]EnergyLog, error) {
	raw, err := c.reportLogsRaw(ctx, deviceID, codes, start, end, energyLogPageSize)
	if err != nil {
		return nil, err
	}
	out := make([]EnergyLog, 0, len(raw))
	for _, m := range raw {
		out = append(out, EnergyLog{
			Code:      stringField(m, "code"),
			EventTime: int64(floatField(m, "event_time")),
			Value:     floatField(m, "value"),
		})
	}
	return out, nil
}

// ReportLog is one entry of the device report log, with the value kept as-is.
// For energy data points (add_ele) it is a number (units of 10^-scale kWh); for
// a lock it is typically a string, such as a fingerprint id ("11") or a boolean
// ("true"/"false"), which is why the raw value is preserved.
type ReportLog struct {
	Code      string
	EventTime int64
	Value     any
}

// ReportLogs fetches report logs for the given DP codes between start and end
// (both Unix milliseconds) keeping the raw value. It is the event source for a
// device the hub cannot stream over the LAN (a sleeping battery lock). The
// response is newest first; callers that need chronological order must sort it.
func (c *Cloud) ReportLogs(ctx context.Context, deviceID string, codes []string, start, end int64, size int) ([]ReportLog, error) {
	raw, err := c.reportLogsRaw(ctx, deviceID, codes, start, end, size)
	if err != nil {
		return nil, err
	}
	out := make([]ReportLog, 0, len(raw))
	for _, m := range raw {
		out = append(out, ReportLog{
			Code:      stringField(m, "code"),
			EventTime: int64(floatField(m, "event_time")),
			Value:     m["value"],
		})
	}
	return out, nil
}

// OpenRecord is one unlocking record of a smart lock from the Smart Lock Open
// Service (the /door-lock/open-logs endpoint). At is the device timestamp in
// Unix milliseconds; Code/Value name the method that was used (for example
// unlock_fingerprint / the fingerprint id).
type OpenRecord struct {
	At         int64  `json:"at"`
	Code       string `json:"code"`
	Value      string `json:"value"`
	UnlockName string `json:"unlock_name,omitempty"`
	UserName   string `json:"user_name,omitempty"`
	UserID     string `json:"user_id,omitempty"`
}

// OpenLogs fetches the unlocking records of a lock between start and end (Unix
// milliseconds). pageNo is 1-based. It returns the records and the total the
// cloud reports for the window. It needs the Smart Lock Open Service
// subscription; without it the cloud answers "uri path invalid".
func (c *Cloud) OpenLogs(ctx context.Context, deviceID string, start, end int64, pageNo, pageSize int) ([]OpenRecord, int, error) {
	if pageNo <= 0 {
		pageNo = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	query := map[string]string{
		"start_time": strconv.FormatInt(start, 10),
		"end_time":   strconv.FormatInt(end, 10),
		"page_no":    strconv.Itoa(pageNo),
		"page_size":  strconv.Itoa(pageSize),
	}
	res, err := c.do(ctx, http.MethodGet, "/v1.0/devices/"+url.PathEscape(deviceID)+"/door-lock/open-logs", query, nil, true)
	if err != nil {
		return nil, 0, err
	}
	result, _ := res["result"].(map[string]any)
	items, _ := result["logs"].([]any)
	out := make([]OpenRecord, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		status, _ := m["status"].(map[string]any)
		out = append(out, OpenRecord{
			At:         int64(floatField(m, "update_time")),
			Code:       stringField(status, "code"),
			Value:      stringField(status, "value"),
			UnlockName: stringField(m, "unlock_name"),
			UserName:   stringField(m, "nick_name"),
			UserID:     stringField(m, "user_id"),
		})
	}
	return out, intField(result, "total"), nil
}

// reportLogsRaw paginates /report-logs and returns the raw entries, so the
// typed wrappers above only differ in how they decode the value.
func (c *Cloud) reportLogsRaw(ctx context.Context, deviceID string, codes []string, start, end int64, size int) ([]map[string]any, error) {
	if size <= 0 {
		size = energyLogPageSize
	}
	query := map[string]string{
		"codes":      strings.Join(codes, ","),
		"start_time": strconv.FormatInt(start, 10),
		"end_time":   strconv.FormatInt(end, 10),
		"size":       strconv.Itoa(size),
	}
	out := []map[string]any{}
	seen := map[string]bool{}
	for page := 0; page < energyLogMaxPages; page++ {
		res, err := c.do(ctx, http.MethodGet, "/v1.0/iot-03/devices/"+url.PathEscape(deviceID)+"/report-logs", query, nil, true)
		if err != nil {
			return nil, err
		}
		result, _ := res["result"].(map[string]any)
		items, _ := result["list"].([]any)
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				out = append(out, m)
			}
		}
		key := stringField(result, "last_row_key")
		hasMore, _ := result["has_more"].(bool)
		if !hasMore || key == "" || seen[key] {
			break
		}
		seen[key] = true
		query["last_row_key"] = key
	}
	return out, nil
}

func (c *Cloud) do(ctx context.Context, method, path string, query map[string]string, body any, auth bool) (map[string]any, error) {
	return c.doOnce(ctx, method, path, query, body, auth, false)
}

func (c *Cloud) doOnce(ctx context.Context, method, path string, query map[string]string, body any, auth, retried bool) (map[string]any, error) {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	canonical := canonicalQuery(query)
	signPath := path
	if canonical != "" {
		signPath += "?" + canonical
	}

	var token string
	if auth {
		tok, err := c.Token(ctx)
		if err != nil {
			return nil, err
		}
		token = tok
	}

	ts := c.now()
	sign := c.sign(method, signPath, bodyBytes, token, ts)

	reqURL := c.base + path
	if canonical != "" {
		reqURL += "?" + encodeQuery(query)
	}
	var reader io.Reader
	if bodyBytes != nil {
		reader = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("client_id", c.cfg.AccessID)
	req.Header.Set("sign", sign)
	req.Header.Set("t", strconv.FormatInt(ts, 10))
	req.Header.Set("sign_method", "HMAC-SHA256")
	req.Header.Set("mode", "cors")
	if token != "" {
		req.Header.Set("access_token", token)
	}
	if bodyBytes != nil {
		req.Header.Set("Content-type", "application/json")
		req.Header.Set("Signature-Headers", "Content-type")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tuya: cloud request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("tuya: cloud returned an unexpected response (HTTP %d): %s", resp.StatusCode, clip(raw))
	}

	code := intField(out, "code")
	if auth && code == 1010 && !retried {
		// Token expired or revoked; drop it and retry once.
		c.mu.Lock()
		c.token = ""
		c.tokenUntil = time.Time{}
		c.mu.Unlock()
		return c.doOnce(ctx, method, path, query, body, auth, true)
	}

	if success, ok := out["success"].(bool); ok {
		if !success {
			return nil, cloudError(path, code, stringField(out, "msg"))
		}
	} else if code != 0 {
		return nil, cloudError(path, code, stringField(out, "msg"))
	}
	return out, nil
}

// sign builds the HMAC-SHA256 signature for one request.
//
// The signed payload is:
//
//	clientID [+ accessToken] + t + METHOD + "\n" + sha256(body) + "\n"
//	+ [ "Content-type:application/json\n" ] + "\n" + path?query
//
// Note there is no separator between t and the method, and the path includes
// the query string exactly as sent (sorted, not URL-encoded).
func (c *Cloud) sign(method, signPath string, body []byte, token string, ts int64) string {
	sum := sha256.Sum256(body)
	var sb strings.Builder
	sb.WriteString(c.cfg.AccessID)
	if token != "" {
		sb.WriteString(token)
	}
	sb.WriteString(strconv.FormatInt(ts, 10))
	sb.WriteString(method)
	sb.WriteByte('\n')
	sb.WriteString(hex.EncodeToString(sum[:]))
	sb.WriteByte('\n')
	if len(body) > 0 {
		sb.WriteString("Content-type:application/json\n")
	}
	sb.WriteByte('\n')
	sb.WriteString(signPath)

	mac := hmac.New(sha256.New, []byte(c.cfg.AccessSecret))
	mac.Write([]byte(sb.String()))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
}

func (c *Cloud) now() int64 {
	c.mu.Lock()
	off := c.offset
	c.mu.Unlock()
	return time.Now().Add(off).UnixMilli()
}

func canonicalQuery(q map[string]string) string {
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+q[k])
	}
	return strings.Join(parts, "&")
}

func encodeQuery(q map[string]string) string {
	if len(q) == 0 {
		return ""
	}
	values := url.Values{}
	for k, v := range q {
		values.Set(k, v)
	}
	// The cloud signs the query with literal commas and rejects %2C with
	// "sign invalid". codes=add_ele and multi-code filters rely on this.
	return strings.ReplaceAll(values.Encode(), "%2C", ",")
}

func cloudDeviceFrom(m map[string]any) CloudDevice {
	d := CloudDevice{
		ID:        stringField(m, "id"),
		Name:      stringField(m, "name"),
		LocalKey:  stringField(m, "local_key"),
		UID:       stringField(m, "uid"),
		ProductID: stringField(m, "product_id"),
		Category:  stringField(m, "category"),
		IP:        stringField(m, "ip"),
		Version:   stringField(m, "version"),
		Raw:       m,
	}
	if v, ok := m["online"].(bool); ok {
		d.Online = v
	}
	return d
}

func cloudError(path string, code int, msg string) error {
	if msg == "" {
		msg = "unknown error"
	}
	lower := strings.ToLower(msg)
	switch {
	case code == 28841101 || code == 60009001 || strings.Contains(lower, "not subscribed"):
		return fmt.Errorf("%w (code %d: %s)", ErrNotSubscribed, code, msg)
	case code == 1010 || strings.Contains(lower, "permission"):
		return fmt.Errorf("tuya: cloud %s failed (code %d): %s (check credentials and whether the IoT trial expired)", path, code, msg)
	case code == 1004:
		return fmt.Errorf("tuya: cloud %s failed (code %d): %s (check the access secret and that the system clock is correct; a skew over a few minutes breaks the signature)", path, code, msg)
	default:
		return fmt.Errorf("tuya: cloud %s failed (code %d): %s", path, code, msg)
	}
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

func intField(m map[string]any, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

func floatField(m map[string]any, key string) float64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return v
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return 0
}

func clip(b []byte) string {
	const max = 200
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
