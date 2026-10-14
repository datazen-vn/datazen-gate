package conformance

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var placeholderJWT = regexp.MustCompile(`<jwt:([^>]*)>`)

const secretMacDinh = "dz-conformance-hs256-secret-0123456789abcdef"

// mintJWT ký `SUB[;sig=bad][;aud=X][;exp=-N]` bằng HS256 (README gate-v1, mục Request). iat/exp theo ĐỒNG HỒ GIẢ để token luôn "tươi" lúc
// gửi; `sig=bad` ký bằng khoá KHÁC khoá cấu hình.
func mintJWT(spec, secret string, now time.Time) (string, error) {
	cac := strings.Split(spec, ";")
	if slices.Contains(cac[1:], "alg=RS256") {
		// Token RS256 + kid, chữ ký RÁC: chỉ để tới được bước lấy khoá JWKS (danh tính jwt dùng `jwks_url`, không có khoá ký thật).
		b64 := base64.RawURLEncoding
		hdr, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "k1"})
		pl, _ := json.Marshal(map[string]any{"sub": cac[0], "iat": now.Unix(), "exp": now.Unix() + 3600})
		return b64.EncodeToString(hdr) + "." + b64.EncodeToString(pl) + "." + b64.EncodeToString(bytes.Repeat([]byte{7}, 256)), nil
	}
	claims := jwt.MapClaims{"sub": cac[0], "iat": now.Unix(), "exp": now.Unix() + 3600}
	khoa := secret
	for _, p := range cac[1:] {
		k, v, _ := strings.Cut(p, "=")
		switch k {
		case "exp":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return "", fmt.Errorf("placeholder jwt sai exp: %s", spec)
			}
			claims["exp"] = now.Unix() + n
		case "aud":
			claims["aud"] = v
		case "sig":
			if v == "bad" {
				khoa = secret + "-khoa-sai-khac-han"
			}
		}
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(khoa))
}

func mrongJWT(v, secret string, now time.Time) (string, error) {
	var err error
	out := placeholderJWT.ReplaceAllStringFunc(v, func(m string) string {
		tk, e := mintJWT(placeholderJWT.FindStringSubmatch(m)[1], secret, now)
		if e != nil {
			err = e
		}
		return tk
	})
	return out, err
}
