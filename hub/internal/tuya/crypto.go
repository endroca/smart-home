package tuya

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// pkcs7Pad pads b to a multiple of the AES block size.
func pkcs7Pad(b []byte) []byte {
	n := aes.BlockSize - len(b)%aes.BlockSize
	return append(b, bytes.Repeat([]byte{byte(n)}, n)...)
}

// pkcs7Unpad removes and validates PKCS#7 padding.
func pkcs7Unpad(b []byte) ([]byte, error) {
	if len(b) == 0 || len(b)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("tuya: invalid padded length %d", len(b))
	}
	n := int(b[len(b)-1])
	if n < 1 || n > aes.BlockSize || n > len(b) {
		return nil, fmt.Errorf("tuya: invalid padding byte %d", n)
	}
	for _, v := range b[len(b)-n:] {
		if int(v) != n {
			return nil, fmt.Errorf("tuya: invalid padding data")
		}
	}
	return b[:len(b)-n], nil
}

// aesECBEncrypt encrypts with AES in ECB mode. Go's standard library has no
// ECB block mode, so it is applied block by block.
func aesECBEncrypt(key, plain []byte, pad bool) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if pad {
		plain = pkcs7Pad(plain)
	}
	if len(plain) == 0 || len(plain)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("tuya: plaintext length %d is not a multiple of %d", len(plain), aes.BlockSize)
	}
	out := make([]byte, len(plain))
	for i := 0; i < len(plain); i += aes.BlockSize {
		block.Encrypt(out[i:i+aes.BlockSize], plain[i:i+aes.BlockSize])
	}
	return out, nil
}

func aesECBDecrypt(key, ciphertext []byte, unpad bool) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("tuya: ciphertext length %d is not a multiple of %d", len(ciphertext), aes.BlockSize)
	}
	out := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i += aes.BlockSize {
		block.Decrypt(out[i:i+aes.BlockSize], ciphertext[i:i+aes.BlockSize])
	}
	if unpad {
		return pkcs7Unpad(out)
	}
	return out, nil
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// encrypt31Control builds the protocol 3.1 CONTROL payload:
//
//	"3.1" + md5("data="+b64+"||lpv=3.1||"+key)[8:24] + base64(AES-ECB(json))
func encrypt31Control(key, payload []byte) ([]byte, error) {
	enc, err := aesECBEncrypt(key, payload, true)
	if err != nil {
		return nil, err
	}
	b64 := base64.StdEncoding.EncodeToString(enc)
	sum := md5.Sum([]byte("data=" + b64 + "||lpv=3.1||" + string(key)))
	digest := hex.EncodeToString(sum[:])[8:24]
	out := make([]byte, 0, 3+16+len(b64))
	out = append(out, []byte("3.1")...)
	out = append(out, []byte(digest)...)
	out = append(out, []byte(b64)...)
	return out, nil
}

func decrypt31Control(key, payload []byte) ([]byte, error) {
	if !bytes.HasPrefix(payload, []byte("3.1")) {
		return nil, fmt.Errorf("tuya: 3.1 payload without version prefix")
	}
	if len(payload) < 3+16 {
		return nil, fmt.Errorf("tuya: 3.1 payload too short")
	}
	enc, err := base64.StdEncoding.DecodeString(string(payload[3+16:]))
	if err != nil {
		return nil, fmt.Errorf("tuya: 3.1 base64: %w", err)
	}
	return aesECBDecrypt(key, enc, true)
}

func gcmSeal(key, iv, plain, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return g.Seal(nil, iv, plain, aad), nil
}

func gcmOpen(key, iv, ciphertext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return g.Open(nil, iv, ciphertext, aad)
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

func xorBytes(a, b []byte) []byte {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = a[i] ^ b[i]
	}
	return out
}

// deriveSessionKey34 is AES-ECB(realKey, clientNonce XOR deviceNonce) with no
// padding, per the 3.4 session key exchange.
func deriveSessionKey34(realKey, clientNonce, deviceNonce []byte) ([]byte, error) {
	return aesECBEncrypt(realKey, xorBytes(clientNonce, deviceNonce), false)
}

// deriveSessionKey35 is AES-GCM(realKey, IV=clientNonce[:12]) over the XOR of
// the nonces, sliced to [12:28].
func deriveSessionKey35(realKey, clientNonce, deviceNonce []byte) ([]byte, error) {
	sealed, err := gcmSeal(realKey, clientNonce[:12], xorBytes(clientNonce, deviceNonce), nil)
	if err != nil {
		return nil, err
	}
	if len(sealed) < 28 {
		return nil, fmt.Errorf("tuya: short 3.5 session key material")
	}
	// Go's GCM Seal returns ciphertext||tag, while the reference slice is taken
	// from a buffer that also contains the 12-byte IV. The session key is the
	// ciphertext block, i.e. the first 16 bytes of sealed.
	return sealed[:16], nil
}
