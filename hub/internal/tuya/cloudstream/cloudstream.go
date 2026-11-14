// Package cloudstream consumes the Tuya Message Service (a Pulsar-based queue)
// and turns each device status report into a decoded event.
//
// It is transport-only: it knows nothing about the hub's devices. It connects
// with the project credentials, decrypts each message (AES-GCM, as configured
// in the Tuya console) and hands the decoded report to a Handler. The service
// decides which devices care and what to do with the events.
package cloudstream

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/apache/pulsar-client-go/pulsar"
)

// serverURLs maps a Tuya data center code to its Pulsar endpoint.
var serverURLs = map[string]string{
	"us":   "pulsar+ssl://mqe.tuyaus.com:7285/",
	"eu":   "pulsar+ssl://mqe.tuyaeu.com:7285/",
	"cn":   "pulsar+ssl://mqe.tuyacn.com:7285/",
	"in":   "pulsar+ssl://mqe.tuyain.com:7285/",
	"us-e": "pulsar+ssl://mqe-ueaz.tuyaus.com:7285/",
	"eu-w": "pulsar+ssl://mqe-weaz.tuyaeu.com:7285/",
	"sg":   "pulsar+ssl://mqe-sg.iotbing.com:7285/",
}

// ServerURL returns the Pulsar endpoint of a Tuya data center (region).
func ServerURL(region string) (string, bool) {
	u, ok := serverURLs[strings.ToLower(strings.TrimSpace(region))]
	return u, ok
}

// Config configures the listener.
type Config struct {
	AccessID     string
	AccessSecret string
	Region       string
	// Test selects the test channel (out/event-test) instead of production.
	Test bool
	// Logf logs lifecycle and connection problems; defaults to log.Printf.
	Logf func(format string, args ...any)
}

// DP is one data point of a status report. At is the device timestamp in Unix
// milliseconds (the envelope timestamp when the report omits it).
type DP struct {
	Code  string
	Value any
	At    int64
}

// Report is one decoded device status report.
type Report struct {
	DeviceID   string
	ProductKey string
	DataID     string
	// At is the envelope timestamp (ms); per-DP timestamps live in DPs.
	At  time.Time
	DPs []DP
}

// Handler receives each decoded report.
type Handler func(Report)

// Password derives the Pulsar password from the credentials, following Tuya's
// SDK formula: md5(accessId + md5hex(accessSecret))[8:24].
func Password(accessID, accessSecret string) string {
	k := md5.Sum([]byte(accessSecret))
	mix := accessID + hex.EncodeToString(k[:])
	m := md5.Sum([]byte(mix))
	return hex.EncodeToString(m[:])[8:24]
}

// Decrypt decrypts a message body's "data" field. The project is configured
// with AES-GCM: the key is accessSecret[8:24] and the base64 payload is
// nonce(12) || ciphertext || tag(16).
func Decrypt(accessSecret, dataB64 string) ([]byte, error) {
	if len(accessSecret) < 24 {
		return nil, errors.New("cloudstream: access secret is too short for the AES key")
	}
	raw, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return nil, fmt.Errorf("cloudstream: base64: %w", err)
	}
	block, err := aes.NewCipher([]byte(accessSecret[8:24]))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize()+gcm.Overhead() {
		return nil, errors.New("cloudstream: ciphertext is too short")
	}
	nonce := raw[:gcm.NonceSize()]
	tail := raw[gcm.NonceSize():]
	return gcm.Open(nil, nonce, tail, nil)
}

// envelope is the outer message body.
type envelope struct {
	Protocol int    `json:"protocol"`
	Pv       string `json:"pv"`
	T        int64  `json:"t"`
	Data     string `json:"data"`
	Sign     string `json:"sign"`
}

// rawReport is the decrypted status report. Tuya has shipped it both at the top
// level and nested under "data", so decodeReport tries both.
type rawReport struct {
	DeviceID   string `json:"devId"`
	ProductKey string `json:"productKey"`
	DataID     string `json:"dataId"`
	Status     []struct {
		Code  string `json:"code"`
		Value any    `json:"value"`
		T     int64  `json:"t"`
	} `json:"status"`
}

// decodeMessage decodes one Pulsar message payload (the encrypted envelope) into
// a Report. It returns ok=false for anything that is not a decryptable status
// report (a different message type, a poison message).
func decodeMessage(accessSecret string, payload []byte) (Report, bool) {
	var env envelope
	if err := json.Unmarshal(payload, &env); err != nil || env.Data == "" {
		return Report{}, false
	}
	inner, err := Decrypt(accessSecret, env.Data)
	if err != nil {
		return Report{}, false
	}
	at := time.Now()
	if env.T > 0 {
		at = time.UnixMilli(env.T)
	}
	return decodeReport(inner, at)
}

// decodeReport parses the decrypted JSON into a Report.
func decodeReport(inner []byte, at time.Time) (Report, bool) {
	var rr rawReport
	var probe map[string]json.RawMessage
	if json.Unmarshal(inner, &probe) == nil {
		if raw, ok := probe["data"]; ok && len(raw) > 0 && raw[0] == '{' {
			_ = json.Unmarshal(raw, &rr)
		}
	}
	if rr.DeviceID == "" {
		_ = json.Unmarshal(inner, &rr)
	}
	if rr.DeviceID == "" || len(rr.Status) == 0 {
		return Report{}, false
	}
	rep := Report{DeviceID: rr.DeviceID, ProductKey: rr.ProductKey, DataID: rr.DataID, At: at}
	for _, s := range rr.Status {
		t := s.T
		if t == 0 {
			t = at.UnixMilli()
		}
		rep.DPs = append(rep.DPs, DP{Code: s.Code, Value: s.Value, At: t})
	}
	return rep, true
}

// Run connects to the Tuya Message Service and streams decoded reports to h
// until ctx is cancelled. It returns when the context is done or the connection
// fails permanently; a transient failure is returned so the caller can retry.
func Run(ctx context.Context, cfg Config, h Handler) error {
	logf := cfg.Logf
	if logf == nil {
		logf = log.Printf
	}
	if strings.TrimSpace(cfg.AccessID) == "" || strings.TrimSpace(cfg.AccessSecret) == "" {
		return errors.New("cloudstream: access id and secret are required")
	}
	url, ok := ServerURL(cfg.Region)
	if !ok {
		return fmt.Errorf("cloudstream: unknown region %q", cfg.Region)
	}
	env := "event"
	if cfg.Test {
		env = "event-test"
	}
	topic := "persistent://" + cfg.AccessID + "/out/" + env
	subscription := cfg.AccessID + "-sub"

	client, err := pulsar.NewClient(pulsar.ClientOptions{
		URL:               url,
		Authentication:    &provider{id: cfg.AccessID, pwd: Password(cfg.AccessID, cfg.AccessSecret)},
		OperationTimeout:  30 * time.Second,
		ConnectionTimeout: 30 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("cloudstream: new client: %w", err)
	}
	defer client.Close()

	consumer, err := client.Subscribe(pulsar.ConsumerOptions{
		Topic:            topic,
		SubscriptionName: subscription,
		Type:             pulsar.Failover,
	})
	if err != nil {
		return fmt.Errorf("cloudstream: subscribe: %w", err)
	}
	defer consumer.Close()

	logf("[cloudstream] ligado ao Message Service (topic %s, sub %s)", topic, subscription)

	for {
		msg, err := consumer.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("cloudstream: receive: %w", err)
		}
		if rep, ok := decodeMessage(cfg.AccessSecret, msg.Payload()); ok {
			h(rep)
		}
		// Ack everything (even ignored messages) so a poison message or a
		// message type we do not handle cannot loop forever.
		consumer.Ack(msg)
	}
}

// provider implements the Pulsar auth.Provider interface with Tuya's "auth1"
// method: the auth data is {"username": <accessId>, "password": <derived>}.
type provider struct {
	id, pwd string
	t       http.RoundTripper
}

func (p *provider) Init() error  { return nil }
func (p *provider) Name() string { return "auth1" }
func (p *provider) GetTLSCertificate() (*tls.Certificate, error) {
	return nil, nil
}
func (p *provider) GetData() ([]byte, error) {
	return []byte(fmt.Sprintf(`{"username":"%s","password":"%s"}`, p.id, p.pwd)), nil
}
func (p *provider) Close() error { return nil }
func (p *provider) RoundTrip(req *http.Request) (*http.Response, error) {
	return p.t.RoundTrip(req)
}
func (p *provider) Transport() http.RoundTripper             { return p.t }
func (p *provider) WithTransport(tr http.RoundTripper) error { p.t = tr; return nil }
