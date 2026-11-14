package cloudstream

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestPasswordVector(t *testing.T) {
	// Vetor calculado de forma independente (Python), não pela implementação.
	if got := Password("testid", "s3cr3tsecret"); got != "e7c0c6ed7bba64a4" {
		t.Fatalf("Password = %q, want e7c0c6ed7bba64a4", got)
	}
}

func TestServerURL(t *testing.T) {
	if u, ok := ServerURL("US"); !ok || u != "pulsar+ssl://mqe.tuyaus.com:7285/" {
		t.Fatalf("us -> %q %v", u, ok)
	}
	if _, ok := ServerURL("marte"); ok {
		t.Fatal("unknown region should fail")
	}
}

// encryptGCM é o inverso de Decrypt, para montar uma mensagem válida no teste.
func encryptGCM(secret, plain string) string {
	block, _ := aes.NewCipher([]byte(secret[8:24]))
	gcm, _ := cipher.NewGCM(block)
	nonce := []byte("0123456789ab")
	raw := gcm.Seal(nil, nonce, []byte(plain), nil) // ct || tag
	return base64.StdEncoding.EncodeToString(append(nonce, raw...))
}

func TestDecryptRoundTrip(t *testing.T) {
	secret := "0123456789abcdefghijklmn"
	plain := `{"devId":"lock1","status":[{"code":"unlock_fingerprint","value":"11","t":1700086300000}]}`
	out, err := Decrypt(secret, encryptGCM(secret, plain))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != plain {
		t.Fatalf("decrypt = %q", out)
	}
}

func TestDecodeMessageFlat(t *testing.T) {
	secret := "0123456789abcdefghijklmn"
	inner := `{"devId":"lock1","productKey":"pk","dataId":"d1","status":[` +
		`{"code":"unlock_fingerprint","value":"11","t":1700086300000},` +
		`{"code":"residual_electricity","value":70,"t":1700086299000}]}`
	env, _ := json.Marshal(map[string]any{"protocol": 4, "pv": "2.0", "t": 1700086300100, "data": encryptGCM(secret, inner)})

	rep, ok := decodeMessage(secret, env)
	if !ok {
		t.Fatal("não decodificou")
	}
	if rep.DeviceID != "lock1" || rep.ProductKey != "pk" || len(rep.DPs) != 2 {
		t.Fatalf("rep=%+v", rep)
	}
	if rep.DPs[0].Code != "unlock_fingerprint" || rep.DPs[0].Value != "11" || rep.DPs[0].At != 1700086300000 {
		t.Fatalf("dp0=%+v", rep.DPs[0])
	}
}

func TestDecodeMessageNested(t *testing.T) {
	secret := "0123456789abcdefghijklmn"
	inner := `{"data":{"devId":"lock1","status":[{"code":"reverse_lock","value":"true"}]}}`
	env, _ := json.Marshal(map[string]any{"t": 1700086300000, "data": encryptGCM(secret, inner)})

	rep, ok := decodeMessage(secret, env)
	if !ok || rep.DeviceID != "lock1" || len(rep.DPs) != 1 {
		t.Fatalf("nested não decodificou: %+v ok=%v", rep, ok)
	}
	// Um DP sem "t" usa o timestamp do envelope.
	if rep.DPs[0].At != 1700086300000 {
		t.Fatalf("fallback de timestamp falhou: %d", rep.DPs[0].At)
	}
}

func TestDecodeMessageRejectsGarbage(t *testing.T) {
	secret := "0123456789abcdefghijklmn"
	if _, ok := decodeMessage(secret, []byte("nao é json")); ok {
		t.Fatal("lixo não deveria decodificar")
	}
	env, _ := json.Marshal(map[string]any{"data": "###"})
	if _, ok := decodeMessage(secret, env); ok {
		t.Fatal("cifra inválida não deveria decodificar")
	}
}
