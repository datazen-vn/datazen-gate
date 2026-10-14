package danhtinh

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

var (
	cookieTenRe = regexp.MustCompile(`^[\x21-\x7e]+$`)
	headerTenRe = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	bearerRe    = regexp.MustCompile(`(?i)^Bearer +(\S+)$`)
)

func boLayToken(tu NguonToken) (func(*http.Request) string, error) {
	switch tu.Loai {
	case "", "authorization":
		return func(r *http.Request) string {
			if m := bearerRe.FindStringSubmatch(r.Header.Get("Authorization")); m != nil {
				return m[1]
			}
			return ""
		}, nil
	case "cookie":
		ten := tu.Ten
		if !cookieTenRe.MatchString(ten) || strings.ContainsAny(ten, "=;,\" \t") {
			return nil, errors.New("jwt.from.cookie phải là tên cookie hợp lệ")
		}
		return func(r *http.Request) string {
			for _, h := range r.Header.Values("Cookie") {
				for _, phan := range strings.Split(h, ";") {
					eq := strings.IndexByte(phan, '=')
					if eq > 0 && strings.TrimSpace(phan[:eq]) == ten {
						v := strings.TrimSpace(phan[eq+1:])
						if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
							v = v[1 : len(v)-1]
						}
						return v
					}
				}
			}
			return ""
		}, nil
	case "header":
		ten := strings.ToLower(tu.Ten)
		if !headerTenRe.MatchString(ten) {
			return nil, errors.New("jwt.from.header phải là tên header hợp lệ")
		}
		return func(r *http.Request) string { return strings.TrimSpace(r.Header.Get(ten)) }, nil
	}
	return nil, errors.New(`jwt.from phải là "authorization", "cookie" hoặc "header"`)
}

// loaiLoiToken: nhãn ngắn, KHÔNG chứa nội dung token (log an toàn).
func loaiLoiToken(err error) string {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return "hết hạn"
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return "chữ ký sai"
	case errors.Is(err, jwt.ErrTokenInvalidAudience):
		return "aud sai"
	case errors.Is(err, jwt.ErrTokenInvalidIssuer):
		return "iss sai"
	case errors.Is(err, jwt.ErrTokenRequiredClaimMissing):
		return "thiếu claim bắt buộc"
	case errors.Is(err, jwt.ErrTokenNotValidYet):
		return "chưa tới nbf"
	}
	return "token không hợp lệ"
}

// layChuThe lấy claim chủ thể: chuỗi hoặc số nguyên an toàn (≤ 2^53−1) ⇒ chuỗi. Thiếu/kiểu khác ⇒ ErrTokenSai.
func layChuThe(claims jwt.MapClaims, ten string) (string, error) {
	v, co := claims[ten]
	if !co {
		return "", fmt.Errorf("%w: token thiếu claim chủ thể", ErrTokenSai)
	}
	switch x := v.(type) {
	case string:
		return x, nil
	case json.Number:
		n, err := strconv.ParseInt(x.String(), 10, 64)
		if err == nil && n >= -(1<<53-1) && n <= 1<<53-1 {
			return strconv.FormatInt(n, 10), nil
		}
	}
	return "", fmt.Errorf("%w: claim chủ thể không phải chuỗi/số nguyên", ErrTokenSai)
}
