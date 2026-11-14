package tuya

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
)

const (
	testKeyHex  = "30313233343536373839616263646566" // "0123456789abcdef"
	testSessHex = "66656463626139383736353433323130" // "fedcba9876543210"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	return b
}

func testKey(t *testing.T) []byte  { return mustHex(t, testKeyHex) }
func testSess(t *testing.T) []byte { return mustHex(t, testSessHex) }

func TestParse33Query(t *testing.T) {
	frame := mustHex(t, "000055aa0000002a0000000a0000004a7b2267774964223a22646576313233222c226465764964223a22646576313233222c22756964223a22646576313233222c2274223a2231373030303030303030227da471b3f60000aa55")
	msg, err := parse55AA(frame, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Seq != 42 || msg.Cmd != cmdDPQuery {
		t.Fatalf("got seq=%d cmd=0x%02x", msg.Seq, msg.Cmd)
	}
	var root map[string]any
	if err := json.Unmarshal(msg.Payload, &root); err != nil {
		t.Fatal(err)
	}
	if root["devId"] != "dev123" {
		t.Fatalf("unexpected payload: %s", msg.Payload)
	}
}

func TestParse33Control(t *testing.T) {
	frame := mustHex(t, "000055aa0000002b0000000700000067332e33000000000000000000000000d136205043d3aaf4a21fdd2b78ba3af8bcebef0e364fb8ebcd4fb9c2fbbe35c4f9645c25b6fa8a9bfbe41e4dd4d1f3302125e1846bd4f824d1b2e468c239926631836d84bd28cb4b20619eb84882ca2a29eaa3140000aa55")
	msg, err := parse55AA(frame, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	js, err := decodePayload(V33, testKey(t), msg.Cmd, msg.Payload, false)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(js, &root); err != nil {
		t.Fatalf("decoded payload: %s", js)
	}
	dps := root["dps"].(map[string]any)
	if dps["1"] != true {
		t.Fatalf("unexpected dps: %v", dps)
	}
}

func TestParse31Control(t *testing.T) {
	frame := mustHex(t, "000055aa0000002c0000000700000087332e313934383236363132383937613066323930545967554550547176536948393072654c6f362b4c7a723777343254376a727a552b357776752b4e6354355a46776c7476714b6d2f766b486b335530664d7749535868684776552b4354527375526f776a6d535a6a4744625953394b4d744c494747657545694379696f3d221444c80000aa55")
	msg, err := parse55AA(frame, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	js, err := decodePayload(V31, testKey(t), msg.Cmd, msg.Payload, false)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(js, &root); err != nil {
		t.Fatalf("decoded payload: %s", js)
	}
	if root["dps"].(map[string]any)["1"] != true {
		t.Fatalf("unexpected payload: %s", js)
	}
}

func TestParse34Control(t *testing.T) {
	frame := mustHex(t, "000055aa0000002d0000000d000000746a2f4275da435008b5b5d1f2e19bf95d9fabc506b104548101bafb6638e0886b1e4c9dd4fafe30e6932f5acf9b6f4646472563a0fe8ddb6914321529740d6ae08e7ce1dafdf623cb20c0a8df8172887ae6e9b4f63c2981e848f90bac32cb2e50669a883b42695b88acc7cb22fd9566cf0000aa55")
	msg, err := parse55AA(frame, testSess(t), false)
	if err != nil {
		t.Fatal(err)
	}
	js, err := decodePayload(V34, testSess(t), msg.Cmd, msg.Payload, false)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(js, &root); err != nil {
		t.Fatalf("decoded payload: %s", js)
	}
	if root["data"].(map[string]any)["dps"].(map[string]any)["1"] != true {
		t.Fatalf("unexpected payload: %s", js)
	}
}

func TestParse35Control(t *testing.T) {
	frame := mustHex(t, "0000669900000000002e0000000d000000621ca0055e13fa4d3cceba9f6abe918a4e94d23bbedb217226e4c90e9c672b01edf1f26b2e9fb6236be76b2d30d2a26659894fa9be4a40cfff69a9fd5e0c6fbb72c79fe5b6d5922b46eea33b22d03ed205e3e799dbdf16c7c7333120e47ec8292f698200009966")
	msg, err := parse6699(frame, testSess(t), false)
	if err != nil {
		t.Fatal(err)
	}
	js, err := decodePayload(V35, testSess(t), msg.Cmd, msg.Payload, false)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(js, &root); err != nil {
		t.Fatalf("decoded payload: %s", js)
	}
	if root["data"].(map[string]any)["dps"].(map[string]any)["1"] != true {
		t.Fatalf("unexpected payload: %s", js)
	}
}

func TestParseHandshakeNonces(t *testing.T) {
	f34 := mustHex(t, "000055aa0000002f0000000300000044a07999f0e2bfbe16f99593e984a449b7377222e061a924c591cd9c27ea163ed4f154f42774f0846f900c3dba6f4ed2161b37d66affd9604ddef6968b12b049ab0000aa55")
	msg, err := parse55AA(f34, testKey(t), false)
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := aesECBDecrypt(testKey(t), msg.Payload, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(nonce, []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}) {
		t.Fatalf("3.4 nonce = %x", nonce)
	}

	f35 := mustHex(t, "00006699000000000030000000030000002c3b172ab50a3a161ef25273ce58e969ebd496aee8bd28ecb024c647b0a8e34d0fde68ec19435f1bdec57d369300009966")
	msg35, err := parse6699(f35, testKey(t), false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(msg35.Payload, nonce) {
		t.Fatalf("3.5 nonce = %x", msg35.Payload)
	}
}

func TestSessionKeyDerivation(t *testing.T) {
	clientNonce := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	deviceNonce := []byte{16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31}
	got34, err := deriveSessionKey34(testKey(t), clientNonce, deviceNonce)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got34, mustHex(t, "377222e061a924c591cd9c27ea163ed4")) {
		t.Fatalf("3.4 session key = %x", got34)
	}
	got35, err := deriveSessionKey35(testKey(t), clientNonce, deviceNonce)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got35, mustHex(t, "ed2fd40d0e642b26f836170ad08ba03d")) {
		t.Fatalf("3.5 session key = %x", got35)
	}
}

func TestReferenceFixtures(t *testing.T) {
	t.Run("3.3 query is encrypted without header", func(t *testing.T) {
		payload := []byte(`{"gwId":"dev123","devId":"dev123","uid":"dev123","t":"1700000000"}`)
		enc, err := encodePayload(V33, testKey(t), cmdDPQuery, payload)
		if err != nil {
			t.Fatal(err)
		}
		frame, err := packFrame(V33, testKey(t), 50, cmdDPQuery, enc)
		if err != nil {
			t.Fatal(err)
		}
		want := "000055aa000000320000000a00000058871ca7eb09f5f78b6aca2505b09fdff605ac8c05ec576d86563a2304cd74431fbcebef0e364fb8ebcd4fb9c2fbbe35c4f9645c25b6fa8a9bfbe41e4dd4d1f3300efea48a0091daa595a3a780fe13bd0eb4d38a390000aa55"
		if got := hex.EncodeToString(frame); got != want {
			t.Fatalf("frame\n got %s\nwant %s", got, want)
		}
	})

	decodes := []struct {
		name  string
		v     Version
		key   []byte
		frame string
		cmd   uint32
		hdr   bool
		reply bool
	}{
		{"3.1 status", V31, testKey(t), "000055aa000000390000000800000035000000007b226465764964223a22646576313233222c22647073223a7b2231223a747275652c2239223a307d7df8a62b6a0000aa55", cmdStatus, false, true},
		{"3.3 encrypted status", V33, testKey(t), "000055aa00000033000000080000004b00000000332e33000000000000000000000000d136205043d3aaf4a21fdd2b78ba3af82125e1846bd4f824d1b2e468c23992667ccda3875adaa38a50474f433d13762ad95f457e0000aa55", cmdStatus, true, true},
		{"3.3 encrypted heartbeat", V33, testKey(t), "000055aa000000340000000900000038871ca7eb09f5f78b6aca2505b09fdff605ac8c05ec576d86563a2304cd74431f0efea48a0091daa595a3a780fe13bd0ef3bfcd250000aa55", cmdHeartbeat, false, false},
		{"3.4 encrypted status", V34, testSess(t), "000055aa000000360000000800000078000000006a2f4275da435008b5b5d1f2e19bf95d9fabc506b104548101bafb6638e0886b1e4c9dd4fafe30e6932f5acf9b6f4646472563a0fe8ddb6914321529740d6ae0603a1b0eb25a110bfc8dc7efa56860fcbc1d7e5f1bc7ab66cd7f1006d520d4a2b20e8c8e27d93d6866a4d9952f010cd40000aa55", cmdStatus, true, true},
		{"3.4 encrypted heartbeat", V34, testSess(t), "000055aa000000350000000900000054b2a507bf7dfd56ffac4c8fa1cde8784f985e5b90e9d5b4d414a9cf00569e494d9438fffddc66acb6cc21ccac93f4747e2454109dbe2c32577bf382818dac9a4420fb6ddfd696f3ced7bd219baa2ab1400000aa55", cmdHeartbeat, false, false},
		{"3.5 encrypted status", V35, testSess(t), "00006699000000000038000000080000006c005524d9e9ae0fceaf985e440acafcd46dfe71e4870208ed75365f5eefa109e340287617d2190b7e0e55ddbd662931014bb8715528bfefc9974e303e52e53e1c854ff9e165691110703ed8d45fbf89fcefeb74df11751de1bb96025c0c361c974c2cedec453c56b272bfbd6b00009966", cmdStatus, true, true},
		{"3.5 encrypted heartbeat", V35, testSess(t), "00006699000000000037000000090000003ec9f9df1d36e2e880bedf78d7a5dedeaabffbe8bbf6512d10d2da3182cdeb7ef45adb25aa2a2d2a3dece1a9288d46ccb6f6c56b58187115cb2dd82f89376400009966", cmdHeartbeat, false, false},
	}
	for _, tc := range decodes {
		t.Run(tc.name, func(t *testing.T) {
			frame := mustHex(t, tc.frame)
			var msg *Message
			var err error
			if tc.v == V35 {
				msg, err = parse6699(frame, tc.key, tc.reply)
			} else if tc.v.atLeast(V34) {
				msg, err = parse55AA(frame, tc.key, tc.reply)
			} else {
				msg, err = parse55AA(frame, nil, tc.reply)
			}
			if err != nil {
				t.Fatal(err)
			}
			if msg.Cmd != tc.cmd {
				t.Fatalf("cmd = 0x%02x, want 0x%02x", msg.Cmd, tc.cmd)
			}
			js, err := decodePayload(tc.v, tc.key, msg.Cmd, msg.Payload, false)
			if err != nil {
				t.Fatal(err)
			}
			dps, err := ParseDPs(js)
			if err != nil {
				t.Fatalf("payload %s: %v", js, err)
			}
			if tc.hdr {
				if dps["1"] != true || dps["9"] != float64(0) {
					t.Fatalf("dps = %v", dps)
				}
				return
			}
			// Heartbeats carry no DPs, but must decode to valid JSON.
			if _, err := ParseJSON(js); err != nil {
				t.Fatalf("payload %s: %v", js, err)
			}
		})
	}
}

func TestReadMessagePartialTimeout(t *testing.T) {
	r := &partialTimeoutReader{}
	_, err := readMessage(r, V33, testKey(t), true)
	if err == nil {
		t.Fatal("expected a timeout")
	}
	if !errors.Is(err, errPartialFrame) {
		t.Fatalf("expected a partial-frame error, got %v", err)
	}
	if !isTimeout(err) {
		t.Fatalf("expected the wrapped error to remain a timeout, got %v", err)
	}
}

type partialTimeoutReader struct{ sent bool }

func (r *partialTimeoutReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		p[0] = 0x00
		return 1, nil
	}
	return 0, fakeTimeout{}
}

type fakeTimeout struct{}

func (fakeTimeout) Error() string   { return "i/o timeout" }
func (fakeTimeout) Timeout() bool   { return true }
func (fakeTimeout) Temporary() bool { return true }

func TestFrameRoundTrip(t *testing.T) {
	payload := []byte(`{"dps":{"1":true}}`)
	for _, v := range []Version{V33, V34, V35} {
		key := testKey(t)
		enc, err := encodePayload(v, key, cmdControl, payload)
		if err != nil {
			t.Fatalf("%s encode: %v", v, err)
		}
		frame, err := packFrame(v, key, 7, cmdControl, enc)
		if err != nil {
			t.Fatalf("%s pack: %v", v, err)
		}
		msg, err := parseFrameForTest(v, frame, key)
		if err != nil {
			t.Fatalf("%s parse: %v", v, err)
		}
		js, err := decodePayload(v, key, msg.Cmd, msg.Payload, false)
		if err != nil {
			t.Fatalf("%s decode: %v", v, err)
		}
		if !bytes.Equal(js, payload) {
			t.Fatalf("%s payload = %s", v, js)
		}
	}
}

func parseFrameForTest(v Version, frame, key []byte) (*Message, error) {
	if v == V35 {
		return parse6699(frame, key, false)
	}
	var hmacKey []byte
	if v.atLeast(V34) {
		hmacKey = key
	}
	return parse55AA(frame, hmacKey, false)
}

func TestTamperDetection(t *testing.T) {
	// 3.3: flip one ciphertext byte and expect a CRC failure.
	frame := mustHex(t, "000055aa0000002b0000000700000067332e33000000000000000000000000d136205043d3aaf4a21fdd2b78ba3af8bcebef0e364fb8ebcd4fb9c2fbbe35c4f9645c25b6fa8a9bfbe41e4dd4d1f3302125e1846bd4f824d1b2e468c239926631836d84bd28cb4b20619eb84882ca2a29eaa3140000aa55")
	frame[30] ^= 0xFF
	if _, err := parse55AA(frame, nil, false); err == nil {
		t.Fatal("expected CRC failure")
	}

	// 3.5: flip one ciphertext byte and expect GCM authentication failure.
	gcm := mustHex(t, "0000669900000000002e0000000d000000621ca0055e13fa4d3cceba9f6abe918a4e94d23bbedb217226e4c90e9c672b01edf1f26b2e9fb6236be76b2d30d2a26659894fa9be4a40cfff69a9fd5e0c6fbb72c79fe5b6d5922b46eea33b22d03ed205e3e799dbdf16c7c7333120e47ec8292f698200009966")
	gcm[60] ^= 0xFF
	if _, err := parse6699(gcm, testSess(t), false); err == nil {
		t.Fatal("expected GCM failure")
	}
}

func TestBuildQueryDevice22(t *testing.T) {
	cases := []struct {
		name string
		v    Version
		cmd  uint32
		want string
	}{
		{"3.3 default", V33, cmdDPQuery, `"gwId":"d"`},
		{"3.3 device22", V33, cmdControlNew, `"devId":"d"`},
		{"3.4 default", V34, cmdDPQueryNew, `{}`},
		{"3.4 device22", V34, cmdDPQueryNew, `"dps":{"1":null}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, payload, err := BuildQuery("d", tc.v, tc.name == "3.3 device22" || tc.name == "3.4 device22", nil)
			if err != nil {
				t.Fatal(err)
			}
			if cmd != tc.cmd {
				t.Fatalf("cmd = 0x%02x, want 0x%02x", cmd, tc.cmd)
			}
			if !bytes.Contains(payload, []byte(tc.want)) {
				t.Fatalf("payload = %s, want substring %s", payload, tc.want)
			}
		})
	}
}

func TestPayloadRoundTrip(t *testing.T) {
	cases := []struct {
		v   Version
		key []byte
		cmd uint32
		js  []byte
	}{
		{V31, testKey(t), cmdControl, []byte(`{"devId":"d","uid":"d","t":"1","dps":{"1":true}}`)},
		{V33, testKey(t), cmdControl, []byte(`{"devId":"d","uid":"d","t":"1","dps":{"1":true}}`)},
		{V33, testKey(t), cmdDPQuery, []byte(`{"gwId":"d","devId":"d"}`)},
		{V34, testSess(t), cmdControlNew, []byte(`{"protocol":5,"t":1,"data":{"dps":{"1":true}}}`)},
		{V34, testSess(t), cmdDPQueryNew, []byte(`{}`)},
		{V35, testSess(t), cmdControlNew, []byte(`{"protocol":5,"t":1,"data":{"dps":{"1":true}}}`)},
		{V35, testSess(t), cmdDPQueryNew, []byte(`{}`)},
	}
	for _, tc := range cases {
		enc, err := encodePayload(tc.v, tc.key, tc.cmd, tc.js)
		if err != nil {
			t.Fatalf("%s/%02x encode: %v", tc.v, tc.cmd, err)
		}
		got, err := decodePayload(tc.v, tc.key, tc.cmd, enc, false)
		if err != nil {
			t.Fatalf("%s/%02x decode: %v", tc.v, tc.cmd, err)
		}
		if !bytes.Equal(got, tc.js) {
			t.Fatalf("%s/%02x got %s want %s", tc.v, tc.cmd, got, tc.js)
		}
	}
}
