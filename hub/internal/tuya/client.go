package tuya

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

// errDataUnvalid is returned when a device rejects a DP_QUERY and needs the
// device22 payload style (an explicit DPS list).
var errDataUnvalid = errors.New("tuya: device rejected payload (data unvalid)")

// ErrClosed is returned by operations on a client that has been closed.
var ErrClosed = errors.New("tuya: client closed")

// Config describes one Wi-Fi Tuya device.
type Config struct {
	Name      string
	ID        string
	IP        string
	Port      int
	Key       string
	Version   Version
	Device22  bool
	Timeout   time.Duration
	Heartbeat time.Duration

	// DisableDiscovery stops the automatic rediscovery by device ID when the
	// configured IP stops responding (for example after a DHCP change).
	DisableDiscovery bool

	// DiscoveryTimeout bounds the LAN lookup used to find the device by ID. It
	// defaults to 3s when zero.
	DiscoveryTimeout time.Duration

	// OnIPChange, if set, is called when rediscovery finds a different IP.
	OnIPChange func(oldIP, newIP string)
}

func (c Config) withDefaults() Config {
	if c.Port == 0 {
		c.Port = 6668
	}
	if c.Timeout <= 0 {
		c.Timeout = 3 * time.Second
	}
	if c.Heartbeat <= 0 {
		c.Heartbeat = 15 * time.Second
	}
	return c
}

// Event is a state change pushed by the device.
type Event struct {
	At      time.Time
	Kind    string // "status", "dp", "ack"
	DPs     map[string]any
	Raw     map[string]any
	Retcode uint32
}

// Result is the decoded reply to a status or control request.
type Result struct {
	Version Version
	Seq     uint32
	Retcode uint32
	DPs     map[string]any
	JSON    map[string]any
	Raw     []byte
}

// Client is a persistent local connection to one Tuya Wi-Fi device.
//
// In the intended use (listening for a wall switch), Listen owns the read side
// of the connection. Status and Set can be used on a freshly dialed client;
// do not call them concurrently with Listen.
type Client struct {
	cfg        Config
	conn       net.Conn
	seq        uint32
	sessionKey []byte
	closed     bool

	mu      sync.Mutex // guards conn, sessionKey and closed
	writeMu sync.Mutex
}

// Dial connects to the device and performs the 3.4/3.5 session handshake when
// needed.
func Dial(ctx context.Context, cfg Config) (*Client, error) {
	cfg = cfg.withDefaults()
	if len(cfg.Key) != 16 {
		return nil, fmt.Errorf("tuya: local key must be 16 bytes, got %d", len(cfg.Key))
	}
	if cfg.Version == "" {
		return nil, errors.New("tuya: protocol version is required; use Detect or set it explicitly")
	}
	c := &Client{cfg: cfg}
	if err := c.connect(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// Detect tries each protocol revision and returns the one that answers a
// status query. It is intentionally conservative: each attempt uses a short
// timeout and closes its connection. The most common revisions are tried first
// so a reachable device is identified in the first attempt.
func Detect(ctx context.Context, cfg Config) (Version, error) {
	base := cfg
	base = base.withDefaults()
	if base.Timeout > 2*time.Second {
		base.Timeout = 2 * time.Second
	}
	for _, v := range []Version{V35, V33, V34, V31, V32} {
		attempt := base
		attempt.Version = v
		c, err := Dial(ctx, attempt)
		if err != nil {
			continue
		}
		_, err = c.Status(ctx)
		c.Close()
		if err == nil {
			return v, nil
		}
	}
	return "", errors.New("tuya: could not detect protocol version (check IP, key and that the Tuya app is closed)")
}

func (c *Client) connect(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	connected := c.conn != nil
	c.mu.Unlock()
	if connected {
		return nil
	}

	var firstErr error
	if c.cfg.IP == "" {
		// Without an address there is nothing to dial; go straight to discovery.
		firstErr = fmt.Errorf("tuya: no LAN address known for device %s", c.cfg.ID)
	} else {
		firstErr = c.open(ctx)
		if firstErr == nil {
			return nil
		}
	}
	if c.cfg.DisableDiscovery || c.cfg.ID == "" {
		return firstErr
	}

	newIP, err := c.findIP(ctx)
	if err != nil {
		return fmt.Errorf("%w; discovery by id %s failed: %v", firstErr, c.cfg.ID, err)
	}
	if newIP == "" || newIP == c.cfg.IP {
		return firstErr
	}
	oldIP := c.cfg.IP
	c.cfg.IP = newIP
	if c.cfg.OnIPChange != nil {
		c.cfg.OnIPChange(oldIP, newIP)
	}
	if err := c.open(ctx); err != nil {
		return fmt.Errorf("tuya: dial %s failed (%v); rediscovered at %s but reconnect failed: %w", oldIP, firstErr, newIP, err)
	}
	return nil
}

// open dials the configured address and performs the protocol handshake.
func (c *Client) open(ctx context.Context) error {
	d := net.Dialer{Timeout: c.cfg.Timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(c.cfg.IP, strconv.Itoa(c.cfg.Port)))
	if err != nil {
		return fmt.Errorf("tuya: dial %s: %w", c.cfg.IP, err)
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = conn.Close()
		return ErrClosed
	}
	if c.conn != nil {
		c.mu.Unlock()
		_ = conn.Close()
		return nil
	}
	c.conn = conn
	c.sessionKey = nil
	c.mu.Unlock()

	if c.cfg.Version.atLeast(V34) {
		hsCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
		defer cancel()
		if err := c.negotiate(hsCtx); err != nil {
			c.closeConn()
			return err
		}
	}
	return nil
}

// discoverFn performs the LAN lookup used to recover from an IP change. It is a
// package variable so tests can replace it with a deterministic stub.
var discoverFn = Discover

// findIP asks the LAN for the device ID and returns the current address.
func (c *Client) findIP(ctx context.Context) (string, error) {
	timeout := c.cfg.DiscoveryTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	devices, err := discoverFn(ctx, timeout)
	if err != nil {
		return "", err
	}
	for _, d := range devices {
		if d.ID == c.cfg.ID && d.IP != "" {
			return d.IP, nil
		}
	}
	return "", fmt.Errorf("tuya: device %s not found by discovery", c.cfg.ID)
}

func (c *Client) closeConn() {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.sessionKey = nil
	c.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

// Close releases the connection. It is safe to call concurrently with Listen
// and makes subsequent connects fail with ErrClosed.
func (c *Client) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.closeConn()
	return nil
}

// connSnapshot returns the current connection, if any. Callers keep using the
// returned net.Conn; Close will make pending reads and writes fail.
func (c *Client) connSnapshot() net.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn
}

func (c *Client) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *Client) key() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessionKey != nil {
		return c.sessionKey
	}
	return []byte(c.cfg.Key)
}

func (c *Client) nextSeq() uint32 {
	c.seq++
	return c.seq
}

func (c *Client) write(frame []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	conn := c.connSnapshot()
	if conn == nil {
		return errors.New("tuya: not connected")
	}
	_ = conn.SetWriteDeadline(time.Now().Add(c.cfg.Timeout))
	_, err := conn.Write(frame)
	return err
}

func (c *Client) send(cmd uint32, payload []byte) (uint32, error) {
	key := c.key()
	wire, err := encodePayload(c.cfg.Version, key, cmd, payload)
	if err != nil {
		return 0, err
	}
	seq := c.nextSeq()
	frame, err := packFrame(c.cfg.Version, key, seq, cmd, wire)
	if err != nil {
		return 0, err
	}
	if err := c.write(frame); err != nil {
		return 0, err
	}
	return seq, nil
}

// negotiate implements the 3-message 3.4/3.5 session key exchange.
func (c *Client) negotiate(ctx context.Context) error {
	realKey := []byte(c.cfg.Key)
	clientNonce, err := randomBytes(16)
	if err != nil {
		return err
	}

	startPayload := clientNonce
	if c.cfg.Version == V34 {
		startPayload, err = aesECBEncrypt(realKey, clientNonce, true)
		if err != nil {
			return err
		}
	}
	if err := c.writeFrame(ctx, realKey, cmdSessStart, startPayload); err != nil {
		return err
	}

	msg, err := c.readOne(ctx, realKey)
	if err != nil {
		return fmt.Errorf("tuya: handshake start: %w", err)
	}
	if msg.Cmd != cmdSessResp {
		return fmt.Errorf("tuya: handshake expected 0x%02x, got 0x%02x", cmdSessResp, msg.Cmd)
	}
	resp := msg.Payload
	if c.cfg.Version == V34 {
		resp, err = aesECBDecrypt(realKey, resp, true)
		if err != nil {
			return fmt.Errorf("tuya: handshake decrypt: %w", err)
		}
	}
	if len(resp) < 48 {
		return fmt.Errorf("tuya: handshake response too short: %d", len(resp))
	}
	deviceNonce := resp[:16]
	if !equalBytes(hmacSHA256(realKey, clientNonce), resp[16:48]) {
		return errors.New("tuya: handshake HMAC mismatch (wrong local key?)")
	}

	finish := hmacSHA256(realKey, deviceNonce)
	if c.cfg.Version == V34 {
		finish, err = aesECBEncrypt(realKey, finish, true)
		if err != nil {
			return err
		}
	}
	if err := c.writeFrame(ctx, realKey, cmdSessFinish, finish); err != nil {
		return err
	}

	var sessionKey []byte
	if c.cfg.Version == V34 {
		sessionKey, err = deriveSessionKey34(realKey, clientNonce, deviceNonce)
	} else {
		sessionKey, err = deriveSessionKey35(realKey, clientNonce, deviceNonce)
	}
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.sessionKey = sessionKey
	c.mu.Unlock()
	return nil
}

func (c *Client) writeFrame(ctx context.Context, key []byte, cmd uint32, payload []byte) error {
	seq := c.nextSeq()
	frame, err := packFrame(c.cfg.Version, key, seq, cmd, payload)
	if err != nil {
		return err
	}
	return c.write(frame)
}

func (c *Client) readOne(ctx context.Context, key []byte) (*Message, error) {
	deadline := time.Now().Add(c.cfg.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn := c.connSnapshot()
	if conn == nil {
		return nil, errors.New("tuya: not connected")
	}
	_ = conn.SetReadDeadline(deadline)
	return readMessage(conn, c.cfg.Version, key, true)
}

// Status sends a DP_QUERY and returns the decoded state.
func (c *Client) Status(ctx context.Context) (*Result, error) {
	res, err := c.statusOnce(ctx)
	if err != nil && !c.cfg.Device22 && errors.Is(err, errDataUnvalid) {
		// Some 3.3/3.4 devices require an explicit DPS list (device22 style).
		c.cfg.Device22 = true
		return c.statusOnce(ctx)
	}
	return res, err
}

func (c *Client) statusOnce(ctx context.Context) (*Result, error) {
	cmd, payload, err := BuildQuery(c.cfg.ID, c.cfg.Version, c.cfg.Device22, nil)
	if err != nil {
		return nil, err
	}
	seq, err := c.send(cmd, payload)
	if err != nil {
		return nil, err
	}
	return c.receive(ctx, seq, cmd)
}

// Set writes one DP and returns the acknowledgement.
func (c *Client) Set(ctx context.Context, dp string, value any) (*Result, error) {
	cmd, payload, err := BuildControl(c.cfg.ID, c.cfg.Version, dp, value)
	if err != nil {
		return nil, err
	}
	seq, err := c.send(cmd, payload)
	if err != nil {
		return nil, err
	}
	return c.receive(ctx, seq, cmd)
}

// Query sends a DP_QUERY without waiting for the reply. It is meant to be used
// while Listen owns the connection: the reply is read by the listen loop and
// emitted as a dp event, so the state can be refreshed on the single TCP
// connection a Tuya device usually allows.
func (c *Client) Query() error {
	cmd, payload, err := BuildQuery(c.cfg.ID, c.cfg.Version, c.cfg.Device22, nil)
	if err != nil {
		return err
	}
	_, err = c.send(cmd, payload)
	return err
}

// SetNoWait writes one DP without waiting for the acknowledgement, for the same
// single-connection reason as Query. The reply (usually the new DP value) is
// read by the listen loop.
func (c *Client) SetNoWait(dp string, value any) error {
	cmd, payload, err := BuildControl(c.cfg.ID, c.cfg.Version, dp, value)
	if err != nil {
		return err
	}
	_, err = c.send(cmd, payload)
	return err
}

func (c *Client) receive(ctx context.Context, seq, cmd uint32) (*Result, error) {
	deadline := time.Now().Add(c.cfg.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn := c.connSnapshot()
	if conn == nil {
		return nil, errors.New("tuya: not connected")
	}
	_ = conn.SetReadDeadline(deadline)

	for {
		msg, err := readMessage(conn, c.cfg.Version, c.key(), true)
		if err != nil {
			return nil, err
		}
		if len(msg.Payload) == 0 {
			if msg.Cmd == cmd || msg.Seq == seq {
				return &Result{Version: c.cfg.Version, Seq: msg.Seq, Retcode: msg.Retcode}, nil
			}
			continue
		}
		js, derr := decodePayload(c.cfg.Version, c.key(), msg.Cmd, msg.Payload, c.cfg.Device22)
		if derr != nil {
			if msg.Cmd == cmd || msg.Seq == seq {
				return nil, derr
			}
			continue
		}
		if isUnvalid(js) {
			return nil, errDataUnvalid
		}
		dps, _ := ParseDPs(js)
		root, _ := ParseJSON(js)
		if msg.Cmd == cmd || msg.Seq == seq || dps != nil {
			return &Result{
				Version: c.cfg.Version,
				Seq:     msg.Seq,
				Retcode: msg.Retcode,
				DPs:     dps,
				JSON:    root,
				Raw:     js,
			}, nil
		}
		if time.Now().After(deadline) {
			return nil, context.DeadlineExceeded
		}
	}
}

// Listen connects, emits the initial snapshot and then streams DP updates.
// It reconnects with exponential backoff until ctx is cancelled.
func (c *Client) Listen(ctx context.Context, fn func(Event)) error {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.mu.Lock()
		connected := c.conn != nil
		c.mu.Unlock()
		if !connected {
			if err := c.connect(ctx); err != nil {
				if errors.Is(err, ErrClosed) {
					return err
				}
				if !sleepCtx(ctx, backoff) {
					return ctx.Err()
				}
				backoff = nextBackoff(backoff)
				continue
			}
			backoff = time.Second
		}
		err := c.listenOnce(ctx, fn)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if c.isClosed() {
			return ErrClosed
		}
		if err != nil {
			c.closeConn()
			if !sleepCtx(ctx, backoff) {
				return ctx.Err()
			}
			backoff = nextBackoff(backoff)
		}
	}
}

func (c *Client) listenOnce(ctx context.Context, fn func(Event)) error {
	snapshot, err := c.Status(ctx)
	if err != nil {
		return err
	}
	if len(snapshot.DPs) > 0 {
		fn(Event{At: time.Now(), Kind: "status", DPs: snapshot.DPs, Raw: snapshot.JSON})
	}

	hbCmd, hbPayload, err := BuildHeartbeat(c.cfg.ID)
	if err != nil {
		return err
	}
	conn := c.connSnapshot()
	if conn == nil {
		return errors.New("tuya: not connected")
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = conn.SetReadDeadline(time.Now().Add(c.cfg.Heartbeat))
		msg, err := readMessage(conn, c.cfg.Version, c.key(), true)
		if err != nil {
			if isTimeout(err) && !errors.Is(err, errPartialFrame) {
				if _, serr := c.send(hbCmd, hbPayload); serr != nil {
					return serr
				}
				continue
			}
			return err
		}
		if len(msg.Payload) == 0 {
			continue
		}
		js, derr := decodePayload(c.cfg.Version, c.key(), msg.Cmd, msg.Payload, c.cfg.Device22)
		if derr != nil {
			continue
		}
		if isUnvalid(js) {
			continue
		}
		dps, _ := ParseDPs(js)
		if len(dps) == 0 {
			continue
		}
		root, _ := ParseJSON(js)
		fn(Event{At: time.Now(), Kind: "dp", DPs: dps, Raw: root, Retcode: msg.Retcode})
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func nextBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func isUnvalid(payload []byte) bool {
	return bytes.Contains(payload, []byte("data unvalid"))
}
