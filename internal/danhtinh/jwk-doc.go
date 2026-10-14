package danhtinh

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
)

const rsaToiThieuBit = 2048

type jwkTho struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// DocJWKS đọc tập khoá công khai (RSA ≥ 2048 bit, EC P-256/384/521, Ed25519). Khoá không dùng để ký (`use` ≠ "sig"), kiểu lạ hoặc
// hỏng bị BỎ QUA (không làm hỏng cả tập); không còn khoá nào dùng được ⇒ lỗi.
func DocJWKS(than []byte) ([]khoaJWK, error) {
	var w struct {
		Keys []jwkTho `json:"keys"`
	}
	if err := json.Unmarshal(than, &w); err != nil {
		return nil, errors.New("JWKS không phải JSON {keys:[…]}")
	}
	var out []khoaJWK
	for _, k := range w.Keys {
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		if kj, ok := k.chuyen(); ok {
			out = append(out, kj)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("JWKS không có khoá công khai dùng được")
	}
	return out, nil
}

func b64(s string) ([]byte, bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	return b, err == nil && len(b) > 0
}

func (k jwkTho) chuyen() (khoaJWK, bool) {
	kj := khoaJWK{kid: k.Kid, alg: k.Alg}
	switch k.Kty {
	case "RSA":
		n, ok1 := b64(k.N)
		e, ok2 := b64(k.E)
		if !ok1 || !ok2 {
			return kj, false
		}
		pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
		if pub.N.BitLen() < rsaToiThieuBit || pub.E < 3 {
			return kj, false
		}
		kj.loai, kj.pub = "RSA", pub
	case "EC":
		var duong elliptic.Curve
		switch k.Crv {
		case "P-256":
			duong = elliptic.P256()
		case "P-384":
			duong = elliptic.P384()
		case "P-521":
			duong = elliptic.P521()
		default:
			return kj, false
		}
		x, ok1 := b64(k.X)
		y, ok2 := b64(k.Y)
		if !ok1 || !ok2 {
			return kj, false
		}
		pub := &ecdsa.PublicKey{Curve: duong, X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		if !duong.IsOnCurve(pub.X, pub.Y) {
			return kj, false
		}
		kj.loai, kj.pub = "EC:"+k.Crv, pub
	case "OKP":
		x, ok := b64(k.X)
		if k.Crv != "Ed25519" || !ok || len(x) != ed25519.PublicKeySize {
			return kj, false
		}
		kj.loai, kj.pub = "OKP", ed25519.PublicKey(x)
	default:
		return kj, false
	}
	return kj, true
}
