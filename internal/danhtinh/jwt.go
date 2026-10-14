package danhtinh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const toiDaDoDaiToken = 8192

var (
	thuatToanDoiXung  = []string{"HS256", "HS384", "HS512"}
	thuatToanCongKhai = []string{"RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256", "ES384", "ES512", "EdDSA"}
	claimRe           = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
)

// NguonToken: nơi lấy token. Loai = "authorization" (Bearer) | "cookie" | "header".
type NguonToken struct {
	Loai string
	Ten  string // tên cookie/header
}

// CauHinhJWT của jwt danh tính — app đã phát JWT cho người dùng; gate xác minh chữ ký rồi lấy claim `sub` làm customer_ref.
type CauHinhJWT struct {
	JWKSURL      string // https (http chỉ loopback có cổng); chọn đúng MỘT trong JWKSURL / Secret
	Secret       []byte // khoá chung HMAC ≥ 32 byte; chỉ HS*
	Issuer       string // nên đặt: không đặt thì token của mọi bên cùng ký bằng khoá này đều qua
	Audience     string
	SubjectClaim string // mặc định "sub"
	ThuatToan    []string
	Tu           NguonToken
	DungSai      time.Duration // lệch đồng hồ khi kiểm exp/nbf, 0..60 s; mặc định 5 s
	Timeout      time.Duration
	JWKSCooldown time.Duration
	HTTP         *http.Client
	Now          func() time.Time
}

// JWT là Bo xác minh JWT.
type JWT struct {
	cfg      CauHinhJWT
	doiXung  bool
	parser   *jwt.Parser
	layToken func(*http.Request) string
	jwks     *JWKS
}

// MoiJWT kiểm cấu hình rồi dựng Bo. An toàn: thuật toán là DANH SÁCH ĐÓNG tách theo loại khoá — khoá chung chỉ HS*, khoá công khai
// chỉ RS*/PS*/ES*/EdDSA — nên không có "đổi RS256 thành HS256 bằng khoá công khai" và không có `none`. `exp` bắt buộc (token sống
// mãi là lỗ hổng).
func MoiJWT(cfg CauHinhJWT) (*JWT, error) {
	if (cfg.JWKSURL == "") == (len(cfg.Secret) == 0) {
		return nil, errors.New("jwt cần đúng MỘT trong jwks_url hoặc secret")
	}
	j := &JWT{cfg: cfg, doiXung: len(cfg.Secret) > 0}
	if j.doiXung && len(cfg.Secret) < 32 {
		return nil, errors.New("jwt.secret phải dài >= 32 byte (HMAC cần khoá tối thiểu bằng độ dài băm)")
	}
	cho := thuatToanCongKhai
	mac := []string{"RS256", "ES256"}
	if j.doiXung {
		cho, mac = thuatToanDoiXung, []string{"HS256"}
	}
	if len(cfg.ThuatToan) == 0 {
		j.cfg.ThuatToan = mac
	}
	for _, a := range j.cfg.ThuatToan {
		if !slices.Contains(cho, a) {
			return nil, fmt.Errorf("jwt.algorithms phải là tập con của %s (khoá chung chỉ HS*, JWKS chỉ thuật toán bất đối xứng; không \"none\")", strings.Join(cho, "/"))
		}
	}
	if j.cfg.SubjectClaim == "" {
		j.cfg.SubjectClaim = "sub"
	}
	if !claimRe.MatchString(j.cfg.SubjectClaim) || j.cfg.SubjectClaim == "__proto__" {
		return nil, errors.New("jwt.subject_claim không hợp lệ")
	}
	if j.cfg.DungSai == 0 {
		j.cfg.DungSai = 5 * time.Second
	}
	if j.cfg.DungSai < 0 || j.cfg.DungSai > 60*time.Second {
		return nil, errors.New("jwt.clock_tolerance phải trong [0, 60s]")
	}
	if j.cfg.Now == nil {
		j.cfg.Now = time.Now
	}
	var err error
	if j.layToken, err = boLayToken(cfg.Tu); err != nil {
		return nil, err
	}
	opts := []jwt.ParserOption{
		jwt.WithValidMethods(j.cfg.ThuatToan), jwt.WithLeeway(j.cfg.DungSai), jwt.WithTimeFunc(j.cfg.Now),
		jwt.WithExpirationRequired(), jwt.WithJSONNumber(),
	}
	if cfg.Issuer != "" {
		opts = append(opts, jwt.WithIssuer(cfg.Issuer))
	}
	if cfg.Audience != "" {
		opts = append(opts, jwt.WithAudience(cfg.Audience))
	}
	j.parser = jwt.NewParser(opts...)
	if !j.doiXung {
		j.jwks = MoiJWKS(cfg.JWKSURL, cfg.HTTP, cfg.Timeout, cfgCooldown(cfg.JWKSCooldown), j.cfg.Now)
	}
	return j, nil
}

// cfgCooldown: 0 trong cấu hình = mặc định 30 s (muốn tắt cooldown dùng giá trị rất nhỏ).
func cfgCooldown(d time.Duration) time.Duration {
	if d == 0 {
		return macDinhCooldown
	}
	return d
}

// NhanDien: không có token ⇒ ("", nil). Token sai ⇒ ErrTokenSai (bọc). JWKS không tải được ⇒ *LoiHaTang, KHÔNG BAO GIỜ chấp nhận token
// khi chưa kiểm được chữ ký.
func (j *JWT) NhanDien(ctx context.Context, r *http.Request) (string, error) {
	token := j.layToken(r)
	if token == "" {
		return "", nil
	}
	if len(token) > toiDaDoDaiToken {
		return "", fmt.Errorf("%w: token quá dài", ErrTokenSai)
	}
	claims := jwt.MapClaims{}
	_, err := j.parser.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		alg := t.Method.Alg()
		if j.doiXung {
			return j.cfg.Secret, nil
		}
		kid, _ := t.Header["kid"].(string)
		return j.jwks.Khoa(ctx, kid, alg)
	})
	if err != nil {
		var ha *LoiHaTang
		if errors.As(err, &ha) {
			return "", ha
		}
		return "", fmt.Errorf("%w: %s", ErrTokenSai, loaiLoiToken(err))
	}
	return layChuThe(claims, j.cfg.SubjectClaim)
}
