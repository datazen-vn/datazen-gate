package chay

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/datazen-vn/market-gate/internal/cauhinh"
	"github.com/datazen-vn/market-gate/internal/hub"
	"github.com/datazen-vn/market-gate/internal/vanhanh"
)

// Mã thoát: 0 đạt · 1 cấu hình/chạy SAI (--check liệt kê hết lỗi) · 2 KHÔNG KIỂM ĐƯỢC (không đọc được tệp cấu hình…).
const (
	MaDat      = 0
	MaSai      = 1
	MaKhongDoc = 2
)

const cachDung = `dz-gate: cổng paywall độc lập cho app có sẵn gắn thuê bao DataZen.

  dz-gate --config gate.yaml                  chạy
  dz-gate --check [--config] gate.yaml        kiểm cấu hình, liệt kê MỌI lỗi/cảnh báo rồi thoát (0 hợp lệ · 1 sai · 2 không đọc được)
  dz-gate --healthcheck --config gate.yaml    GET /_dz/healthz của chính tiến trình đang chạy (cho HEALTHCHECK của container; 0 sống · 1 chết)
  dz-gate --version
`

// Main chạy dz-gate theo tham số dòng lệnh; trả mã thoát. `env` tra biến môi trường.
func Main(args []string, env cauhinh.Env, stdout, stderr io.Writer, phienBan string) int {
	fs := flag.NewFlagSet("dz-gate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, cachDung) }
	duong := fs.String("config", "", "đường dẫn tệp cấu hình YAML")
	check := fs.Bool("check", false, "kiểm cấu hình rồi thoát")
	healthcheck := fs.Bool("healthcheck", false, "kiểm tiến trình đang chạy rồi thoát")
	version := fs.Bool("version", false, "in phiên bản")
	if err := fs.Parse(args); err != nil {
		return MaSai
	}
	if *version {
		fmt.Fprintf(stdout, "dz-gate %s\n", phienBan)
		return MaDat
	}
	switch {
	case *duong == "" && fs.NArg() == 1: // `dz-gate --check gate.yaml`
		*duong = fs.Arg(0)
	case *duong == "" || fs.NArg() > 0:
		fs.Usage()
		return MaSai
	}
	f, err := os.Open(*duong)
	if err != nil {
		fmt.Fprintf(stderr, "không đọc được tệp cấu hình: %v\n", err)
		return MaKhongDoc
	}
	cfg, err := cauhinh.Doc(f)
	f.Close()
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return MaSai
	}
	cfg.MacDinh()
	if *healthcheck {
		return kiemSong(cfg.Ops.Listen, stderr)
	}
	kq := cauhinh.Kiem(cfg, env)
	for _, v := range kq.Cac {
		fmt.Fprintln(stderr, v)
	}
	if !kq.HopLe() {
		fmt.Fprintf(stderr, "cấu hình KHÔNG hợp lệ (%d vấn đề)\n", len(kq.Cac))
		return MaSai
	}
	if *check {
		fmt.Fprintf(stdout, "cấu hình hợp lệ (%d cảnh báo)\n", len(kq.Cac))
		return MaDat
	}
	return chay(cfg, env, stderr, phienBan)
}

// kiemSong: GET http://<ops.listen>/_dz/healthz. Địa chỉ trống/0.0.0.0 ⇒ 127.0.0.1.
func kiemSong(listen string, stderr io.Writer) int {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		fmt.Fprintf(stderr, "ops.listen không hợp lệ: %v\n", err)
		return MaSai
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	c := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := c.Get("http://" + net.JoinHostPort(host, port) + "/_dz/healthz")
	if err != nil {
		fmt.Fprintf(stderr, "healthcheck: %v\n", err)
		return MaSai
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "healthcheck: HTTP %d\n", res.StatusCode)
		return MaSai
	}
	return MaDat
}

// chay chạy hai máy chủ (khách + vận hành) tới khi nhận SIGINT/SIGTERM rồi tắt êm tối đa 10 s.
func chay(cfg *cauhinh.Config, env cauhinh.Env, stderr io.Writer, phienBan string) int {
	log := vanhanh.MoiLog(stderr)
	d, err := Lap(cfg, TuyChon{Env: env, PhienBan: phienBan, Log: log})
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return MaSai
	}
	// Khoá API sai thì từ chối chạy ngay (mọi tuyến trả phí sẽ 503): lỗi cấu hình phải hiện lúc khởi động, không phải lúc khách vào.
	// Hub chết/mạng lỗi thì vẫn chạy (fail-closed theo cấu hình), chỉ cảnh báo.
	ctx, dung := context.WithTimeout(context.Background(), 10*time.Second)
	err = d.Cong.Khach.Ping(ctx)
	dung()
	if l, ok := hub.ALoi(err); ok && l.Ma == hub.LoiKhoa {
		log.Error("khoa API bi hub tu choi (hub.api_key_env)", "http", l.HTTP, "hub_code", l.HubCode)
		return MaSai
	}
	if err != nil {
		ma := "loi"
		if l, ok := hub.ALoi(err); ok {
			ma = l.Ma
		}
		log.Warn("hub chua kiem duoc luc khoi dong", "ma", ma)
	}

	chinh := &http.Server{Addr: cfg.Gate.Listen, Handler: d.Chinh, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10}
	ops := &http.Server{Addr: cfg.Ops.Listen, Handler: d.Ops, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	loi := make(chan error, 2)
	for _, s := range []*http.Server{chinh, ops} {
		go func() { loi <- s.ListenAndServe() }()
	}
	log.Info("dz-gate chay", "phien_ban", phienBan, "gate", cfg.Gate.Listen, "ops", cfg.Ops.Listen)

	sig, huy := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer huy()
	ma := MaDat
	select {
	case <-sig.Done():
		log.Info("nhan tin hieu tat, dang tat em")
	case err := <-loi:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("may chu dung bat ngo", "loi", err.Error())
			ma = MaSai
		}
	}
	tat, huyTat := context.WithTimeout(context.Background(), 10*time.Second)
	defer huyTat()
	_ = chinh.Shutdown(tat)
	_ = ops.Shutdown(tat)
	return ma
}
