package cauhinh

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/datazen-vn/market-gate/internal/danhtinh"
	"github.com/datazen-vn/market-gate/internal/hub"
)

func errLoaiDanhTinh(t string) error {
	return fmt.Errorf("type phải là jwt hoặc session_hook, nhận %q", t)
}

func (id Identity) boJWT(env Env, client *http.Client, now func() time.Time) (danhtinh.Bo, error) {
	cfg := danhtinh.CauHinhJWT{
		Issuer: id.Issuer, Audience: id.Audience, SubjectClaim: id.SubjectClaim, ThuatToan: id.Algorithms,
		DungSai: id.ClockTol.D(), Timeout: id.Timeout.D(), HTTP: client, Now: now,
	}
	if id.JWKSURL != "" {
		if _, err := hub.PhanTichURLAnToan(id.JWKSURL, "jwks_url"); err != nil {
			return nil, err
		}
		cfg.JWKSURL = id.JWKSURL
	}
	switch {
	case id.Secret != "" && id.SecretEnv != "":
		return nil, errors.New("chỉ khai MỘT trong secret hoặc secret_env")
	case id.SecretEnv != "":
		v, co := env(id.SecretEnv)
		if !co || v == "" {
			return nil, fmt.Errorf("biến môi trường %s (secret_env) chưa đặt", id.SecretEnv)
		}
		cfg.Secret = []byte(v)
	case id.Secret != "":
		cfg.Secret = []byte(id.Secret)
	}
	tu, err := phanTichNguon(id.From)
	if err != nil {
		return nil, err
	}
	cfg.Tu = tu
	return danhtinh.MoiJWT(cfg)
}

// phanTichNguon: "authorization" (mặc định) | "cookie:<tên>" | "header:<tên>".
func phanTichNguon(s string) (danhtinh.NguonToken, error) {
	if s == "" || s == "authorization" {
		return danhtinh.NguonToken{Loai: "authorization"}, nil
	}
	if ten, ok := strings.CutPrefix(s, "cookie:"); ok {
		return danhtinh.NguonToken{Loai: "cookie", Ten: ten}, nil
	}
	if ten, ok := strings.CutPrefix(s, "header:"); ok {
		return danhtinh.NguonToken{Loai: "header", Ten: ten}, nil
	}
	return danhtinh.NguonToken{}, fmt.Errorf(`from phải là "authorization", "cookie:<tên>" hoặc "header:<tên>", nhận %q`, s)
}

func (id Identity) boSessionHook(client *http.Client, now func() time.Time) (danhtinh.Bo, error) {
	cfg := danhtinh.CauHinhSessionHook{
		URL: id.URL, Forward: id.Forward, SubjectPath: id.SubjectPath, Timeout: id.Timeout.D(),
		AllowHosts: id.AllowHosts, HTTP: client, Now: now,
	}
	if id.CacheTTL != nil {
		if id.CacheTTL.D() == 0 {
			cfg.TatDem = true
		} else {
			cfg.TTL = id.CacheTTL.D()
		}
	}
	return danhtinh.MoiSessionHook(cfg)
}
