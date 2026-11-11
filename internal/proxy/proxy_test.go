package proxy

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/datazen-vn/datazen-gate/internal/cauhinh"
	"github.com/datazen-vn/datazen-gate/internal/hub"
	"github.com/datazen-vn/datazen-gate/internal/quyetdinh"
)

type congGia func(r *http.Request) quyetdinh.KetQua

func (f congGia) XuLy(_ context.Context, r *http.Request) quyetdinh.KetQua { return f(r) }

// Dùng ĐÚNG danh sách mặc định của sản phẩm (cauhinh.XoaMacDinh), không bản sao riêng.
var xoaMacDinh = cauhinh.XoaMacDinh

func choQua(ma string, e *hub.KetQua) congGia {
	return func(*http.Request) quyetdinh.KetQua {
		return quyetdinh.KetQua{Quyet: quyetdinh.Quyet{Cho: true, MaKhach: ma, KetQua: e}}
	}
}

// upstreamGia ghi lại request nhận được.
type upstreamGia struct {
	*httptest.Server
	mu   sync.Mutex
	goi  []*http.Request
	xuLy http.HandlerFunc
}

func moiUpstream(t *testing.T, xuLy http.HandlerFunc) *upstreamGia {
	u := &upstreamGia{xuLy: xuLy}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.goi = append(u.goi, r.Clone(context.Background()))
		u.mu.Unlock()
		if u.xuLy != nil {
			u.xuLy(w, r)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *upstreamGia) cuoi(t *testing.T) *http.Request {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.goi) == 0 {
		t.Fatal("upstream không được gọi")
	}
	return u.goi[len(u.goi)-1]
}

func (u *upstreamGia) soGoi() int { u.mu.Lock(); defer u.mu.Unlock(); return len(u.goi) }

func dungProxy(t *testing.T, u *upstreamGia, cong QuyetDinh, sua func(*Cau)) *httptest.Server {
	up, _ := url.Parse(u.URL)
	c := Cau{Cong: cong, Upstream: up, Xoa: xoaMacDinh}
	if sua != nil {
		sua(&c)
	}
	s := httptest.NewServer(Moi(c))
	t.Cleanup(s.Close)
	return s
}

// guiTho gửi yêu cầu thô và trả phần ĐẦU phản hồi (status + header); không đợi đóng kết nối (tránh phụ thuộc keep-alive).
func guiTho(t *testing.T, addr, req string) string {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = c.Write([]byte(req))
	br := bufio.NewReader(c)
	var sb strings.Builder
	for {
		l, err := br.ReadString('\n')
		sb.WriteString(l)
		if l == "\r\n" || err != nil {
			break
		}
	}
	return sb.String()
}

func TestChanThiGateTuTraVaKhongGoiUpstream(t *testing.T) {
	u := moiUpstream(t, nil)
	var quanSat atomic.Int32
	cong := congGia(func(r *http.Request) quyetdinh.KetQua {
		q := quyetdinh.Quyet{LyDo: quyetdinh.LyDoKhongDanhTinh, Status: 401}
		return quyetdinh.KetQua{Quyet: q, PhanHoi: &quyetdinh.PhanHoi{Status: 401, Header: http.Header{"Cache-Control": {"no-store"}}, Than: []byte(`{"code":"dz_no_identity","status":401}`)}}
	})
	s := dungProxy(t, u, cong, func(c *Cau) {
		c.QuanSat = func(*http.Request, quyetdinh.KetQua, time.Duration) { quanSat.Add(1) }
	})
	res, err := http.Get(s.URL + "/premium/x")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 401 || res.Header.Get("Cache-Control") != "no-store" || !strings.Contains(string(b), "dz_no_identity") {
		t.Fatalf("%d %v %s", res.StatusCode, res.Header, b)
	}
	if u.soGoi() != 0 {
		t.Fatal("chặn thì KHÔNG được gọi upstream")
	}
	if quanSat.Load() != 1 {
		t.Fatalf("QuanSat: %d", quanSat.Load())
	}
}

func TestXoaHeaderGiaMaoCuaClientRoiTiemCuaGate(t *testing.T) {
	u := moiUpstream(t, nil)
	e := &hub.KetQua{TrangThai: hub.Active, MaGoi: "pro", Features: map[string]bool{"premium": true, "export": true, "tat": false, "co,phay": true}, Nguon: hub.NguonMang}
	s := dungProxy(t, u, choQua("cus_that", e), func(c *Cau) { c.Xoa = append(append([]string{}, xoaMacDinh...), "x-noi-bo-*") })
	req, _ := http.NewRequest("GET", s.URL+"/premium/x", nil)
	for k, v := range map[string]string{
		"X-DZ-Customer-Ref": "cus_gia", "x-dz-entitlement-status": "active", "X-Dz-Features": "admin", "X-DZ-Gate-Hop": "0",
		"X-Auth-Request-User": "admin", "X-Auth-Request-Email": "a@b", "X-Forwarded-User": "admin", "X-Noi-Bo-Khoa": "bi-mat",
		"X-DZ-Grace-Until": "2099-01-01T00:00:00Z", "X-DZ-Plan": "enterprise", "X-DZ-Entitlement-Source": "cache",
		"Authorization": "Bearer giu-nguyen", "Cookie": "sid=giu-nguyen", "X-Khac": "giu-nguyen",
	} {
		req.Header.Set(k, v)
	}
	req.Header.Add("X-DZ-Customer-Ref", "cus_gia_2") // nhiều giá trị
	if _, err := http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	}
	got := u.cuoi(t).Header
	cacTruongHop := map[string]string{
		"X-DZ-Customer-Ref": "cus_that", "X-DZ-Entitlement-Status": "active", "X-DZ-Plan": "pro", "X-DZ-Features": "export,premium",
		"X-DZ-Entitlement-Source": "network", "X-DZ-Gate-Hop": "1",
		"Authorization": "Bearer giu-nguyen", "Cookie": "sid=giu-nguyen", "X-Khac": "giu-nguyen",
	}
	for k, v := range cacTruongHop {
		if vs := got.Values(k); len(vs) != 1 || vs[0] != v {
			t.Errorf("%s = %q, mong đúng một giá trị %q", k, vs, v)
		}
	}
	for _, k := range []string{"X-Auth-Request-User", "X-Auth-Request-Email", "X-Forwarded-User", "X-Noi-Bo-Khoa", "X-DZ-Grace-Until"} {
		if got.Get(k) != "" {
			t.Errorf("%s của client không được lọt tới upstream: %q", k, got.Get(k))
		}
	}
}

func TestHeaderGraceUntilChiKhiAnHanVaDinhDangUTC(t *testing.T) {
	u := moiUpstream(t, nil)
	het := time.Date(2026, 10, 16, 17, 0, 0, 0, time.FixedZone("+07", 7*3600))
	e := &hub.KetQua{TrangThai: hub.PastDue, AnHan: true, HetAnHan: &het, HieuLuc: true, Nguon: hub.NguonDem}
	s := dungProxy(t, u, choQua("c", e), nil)
	_, _ = http.Get(s.URL + "/x")
	if g := u.cuoi(t).Header.Get("X-DZ-Grace-Until"); g != "2026-10-16T10:00:00Z" {
		t.Fatalf("Grace-Until = %q", g)
	}
	// không ân hạn ⇒ không có header, dù HetAnHan có giá trị
	e2 := &hub.KetQua{TrangThai: hub.PastDue, HetAnHan: &het, Nguon: hub.NguonDem}
	s2 := dungProxy(t, u, choQua("c", e2), nil)
	_, _ = http.Get(s2.URL + "/x")
	if g := u.cuoi(t).Header.Get("X-DZ-Grace-Until"); g != "" {
		t.Fatalf("không trong ân hạn: %q", g)
	}
}

func TestBypassAnDanhKhongCoHeaderDanhTinh(t *testing.T) {
	u := moiUpstream(t, nil)
	s := dungProxy(t, u, choQua("", nil), nil)
	req, _ := http.NewRequest("GET", s.URL+"/api/public/x", nil)
	req.Header.Set("X-DZ-Customer-Ref", "cus_gia")
	_, _ = http.DefaultClient.Do(req)
	got := u.cuoi(t).Header
	if got.Get("X-DZ-Customer-Ref") != "" || got.Get("X-DZ-Entitlement-Status") != "" {
		t.Fatalf("ẩn danh không có header danh tính/quyền: %v", got)
	}
	if got.Get("X-DZ-Gate-Hop") != "1" {
		t.Fatal("vẫn đánh dấu đã qua gate")
	}
}

func TestFailOpenCoNguonMaKhongCoQuyen(t *testing.T) {
	u := moiUpstream(t, nil)
	cong := congGia(func(*http.Request) quyetdinh.KetQua {
		return quyetdinh.KetQua{Quyet: quyetdinh.Quyet{Cho: true, MaKhach: "cus_1"}, Nguon: hub.NguonMoCua}
	})
	s := dungProxy(t, u, cong, nil)
	_, _ = http.Get(s.URL + "/x")
	got := u.cuoi(t).Header
	if got.Get("X-DZ-Entitlement-Source") != "fail-open" || got.Get("X-DZ-Customer-Ref") != "cus_1" || got.Get("X-DZ-Entitlement-Status") != "" {
		t.Fatalf("%v", got)
	}
}

func TestGiaTriLaBiBoKhongLamHong(t *testing.T) {
	u := moiUpstream(t, nil)
	e := &hub.KetQua{TrangThai: hub.Active, MaGoi: "gói-tiếng-việt", Features: map[string]bool{"ok": true, "bad\x01": true}, Nguon: hub.NguonMang}
	s := dungProxy(t, u, choQua("c", e), nil)
	_, _ = http.Get(s.URL + "/x")
	got := u.cuoi(t).Header
	if got.Get("X-DZ-Plan") != "" || got.Get("X-DZ-Features") != "ok" {
		t.Fatalf("giá trị ngoài ASCII in được bị bỏ: plan %q features %q", got.Get("X-DZ-Plan"), got.Get("X-DZ-Features"))
	}
}

func TestConnectionNeuTenHeaderCuaGateKhongLamMatHeaderTiem(t *testing.T) {
	// Client liệt kê header gate sẽ tiêm trong `Connection:` để proxy xoá nó (mánh hop-by-hop): header của gate vẫn phải tới upstream.
	u := moiUpstream(t, nil)
	e := &hub.KetQua{TrangThai: hub.Active, Nguon: hub.NguonMang}
	s := dungProxy(t, u, choQua("cus_that", e), nil)
	_ = guiTho(t, strings.TrimPrefix(s.URL, "http://"), "GET /x HTTP/1.1\r\nHost: a\r\nConnection: close, X-DZ-Customer-Ref, X-DZ-Gate-Hop, X-Foo\r\nX-DZ-Customer-Ref: cus_gia\r\nX-Foo: 1\r\n\r\n")
	got := u.cuoi(t).Header
	if got.Get("X-DZ-Customer-Ref") != "cus_that" || got.Get("X-DZ-Gate-Hop") != "1" {
		t.Fatalf("header của gate phải tới upstream: %v", got)
	}
	if got.Get("X-Foo") != "" {
		t.Fatal("header nêu trong Connection là hop-by-hop: phải bị bỏ")
	}
}

func TestHopByHopBiBo(t *testing.T) {
	u := moiUpstream(t, nil)
	s := dungProxy(t, u, choQua("c", nil), nil)
	_ = guiTho(t, strings.TrimPrefix(s.URL, "http://"), "GET /x HTTP/1.1\r\nHost: a\r\nConnection: keep-alive, X-Rieng\r\nKeep-Alive: timeout=5\r\nProxy-Authorization: Basic eA==\r\nProxy-Authenticate: x\r\nTe: gzip\r\nTrailer: X-T\r\nX-Rieng: 1\r\nX-Giu: 2\r\n\r\n")
	got := u.cuoi(t).Header
	for _, k := range []string{"Keep-Alive", "Proxy-Authorization", "Proxy-Authenticate", "Te", "Trailer", "X-Rieng"} {
		if got.Get(k) != "" {
			t.Errorf("hop-by-hop %s lọt tới upstream: %q", k, got.Get(k))
		}
	}
	if got.Get("X-Giu") != "2" {
		t.Fatal("header thường phải giữ")
	}
}

func TestXForwardedDungKetNoiThatKhongTinClient(t *testing.T) {
	u := moiUpstream(t, nil)
	s := dungProxy(t, u, choQua("c", nil), nil)
	req, _ := http.NewRequest("GET", s.URL+"/x", nil)
	req.Header.Set("X-Forwarded-For", "6.6.6.6")
	req.Header.Set("X-Forwarded-Host", "ke-tan-cong.test")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Forwarded", "for=6.6.6.6")
	_, _ = http.DefaultClient.Do(req)
	got := u.cuoi(t).Header
	if got.Get("X-Forwarded-For") != "127.0.0.1" || got.Get("Forwarded") != "" || strings.Contains(got.Get("X-Forwarded-Host"), "ke-tan-cong") || got.Get("X-Forwarded-Proto") != "http" {
		t.Fatalf("X-Forwarded-* phải từ kết nối thật: %v", got)
	}
}

// TB-4a: header ghi đè đường dẫn/host làm app chạy đường khác đường gate đã xét (GET /public + X-Original-URL: /premium/x).
func TestHeaderGhiDeDuongDanVaNguonGocBiXoaQuaProxyThat(t *testing.T) {
	u := moiUpstream(t, nil)
	s := dungProxy(t, u, choQua("c", nil), nil)
	spoof := map[string]string{
		"X-Original-URL": "/premium/x", "X-Rewrite-URL": "/premium/x", "X-Original-URI": "/premium/x", "X-Original-Host": "noi-bo.test",
		"X-Host": "noi-bo.test", "X-HTTP-Host-Override": "noi-bo.test", "Client-IP": "6.6.6.6", "X-Originating-IP": "6.6.6.6",
		"Fly-Client-IP": "6.6.6.6", "X-Remote-IP": "6.6.6.6", "X-Remote-Addr": "6.6.6.6",
	}
	req, _ := http.NewRequest("GET", s.URL+"/public", nil)
	for k, v := range spoof {
		req.Header.Set(k, v)
	}
	req.Header.Set("X-Giu-Nguyen", "ok")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	got := u.cuoi(t)
	if got.RequestURI != "/public" {
		t.Fatalf("upstream phải nhận đúng đường dẫn gate đã xét: %q", got.RequestURI)
	}
	for k := range spoof {
		if got.Header.Get(k) != "" {
			t.Errorf("%s lọt tới upstream: %q", k, got.Header.Get(k))
		}
	}
	if got.Header.Get("X-Giu-Nguyen") != "ok" {
		t.Fatalf("header thường phải giữ: %v", got.Header)
	}
}

func TestGiuNguyenDuongDanThoVaQuery(t *testing.T) {
	// Gate quyết định theo đường dẫn THÔ; upstream phải nhận ĐÚNG đường dẫn đó (không bị chuẩn hoá) để hai bên nhìn cùng một thứ.
	u := moiUpstream(t, nil)
	s := dungProxy(t, u, choQua("c", nil), nil)
	for _, p := range []string{"/premium/../public/x", "/x%2Fsecret?a=1&b=%2e", "/%2e%2e/x", "//premium/x", "/a;jsessionid=1/b", "/%41bc", "/PREMIUM/x?checkout_session_id=5b0c2f3e-8d41-4a6b-9c7e-2f1a3b4c5d6e"} {
		got := guiTho(t, strings.TrimPrefix(s.URL, "http://"), "GET "+p+" HTTP/1.1\r\nHost: a\r\nConnection: close\r\n\r\n")
		if !strings.HasPrefix(got, "HTTP/1.1 200") {
			t.Errorf("%s: %.60s", p, got)
			continue
		}
		if ru := u.cuoi(t).RequestURI; ru != p {
			t.Errorf("upstream nhận %q, mong nguyên văn %q", ru, p)
		}
	}
}

func TestPhanHoiUpstreamGiuNguyen(t *testing.T) {
	u := moiUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "a=1; HttpOnly")
		w.Header().Add("Set-Cookie", "b=2")
		w.Header().Set("X-DZ-Customer-Ref", "tu-upstream") // phản hồi của upstream KHÔNG bị gate sửa/xoá
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.WriteHeader(418)
		_, _ = w.Write([]byte("ấm trà"))
	})
	s := dungProxy(t, u, choQua("c", nil), nil)
	res, err := http.Get(s.URL + "/x")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 418 || string(b) != "ấm trà" || len(res.Header.Values("Set-Cookie")) != 2 || res.Header.Get("X-DZ-Customer-Ref") != "tu-upstream" || res.Header.Get("Cache-Control") != "public, max-age=60" {
		t.Fatalf("%d %v %q", res.StatusCode, res.Header, b)
	}
}

func TestUpstreamChet502KhongLoDiaChi(t *testing.T) {
	u := moiUpstream(t, nil)
	var baoLoi atomic.Int32
	s := dungProxy(t, u, choQua("c", nil), func(c *Cau) { c.BaoLoiUpstream = func(error) { baoLoi.Add(1) } })
	u.Close()
	for _, pt := range []string{"GET", "HEAD", "POST"} {
		req, _ := http.NewRequest(pt, s.URL+"/x", strings.NewReader("a"))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 502 || res.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %v", pt, res.StatusCode, res.Header)
		}
		if pt != "HEAD" && (!strings.Contains(string(b), "dz_upstream_unavailable") || strings.Contains(string(b), "127.0.0.1")) {
			t.Fatalf("%s: thân phải là mã ổn định, không lộ địa chỉ: %s", pt, b)
		}
	}
	if baoLoi.Load() != 3 {
		t.Fatalf("BaoLoiUpstream: %d", baoLoi.Load())
	}
}

func TestBodyVaPhuongThucDuocChuyenTiep(t *testing.T) {
	u := moiUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_, _ = w.Write([]byte(r.Method + ":" + string(b)))
	})
	s := dungProxy(t, u, choQua("c", nil), nil)
	res, _ := http.Post(s.URL+"/x", "text/plain", strings.NewReader("noi-dung"))
	b, _ := io.ReadAll(res.Body)
	if string(b) != "POST:noi-dung" {
		t.Fatalf("%s", b)
	}
}

func TestWebSocketUpgradeDiQua(t *testing.T) {
	u := moiUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" || r.Header.Get("X-DZ-Gate-Hop") != "1" {
			w.WriteHeader(400)
			return
		}
		c, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = rw.Flush()
		line, _ := rw.ReadString('\n')
		_, _ = rw.WriteString("echo:" + line)
		_ = rw.Flush()
	})
	s := dungProxy(t, u, choQua("c", nil), nil)
	c, err := net.Dial("tcp", strings.TrimPrefix(s.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = c.Write([]byte("GET /ws HTTP/1.1\r\nHost: a\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n"))
	br := bufio.NewReader(c)
	status, _ := br.ReadString('\n')
	if !strings.HasPrefix(status, "HTTP/1.1 101") {
		t.Fatalf("mong 101, có %q", status)
	}
	for {
		l, _ := br.ReadString('\n')
		if l == "\r\n" || l == "" {
			break
		}
	}
	_, _ = c.Write([]byte("xin chao\n"))
	got, _ := br.ReadString('\n')
	if got != "echo:xin chao\n" {
		t.Fatalf("kênh hai chiều sau 101: %q", got)
	}
}

func TestXoaHeaderTrucTiep(t *testing.T) {
	h := http.Header{}
	h.Set("X-DZ-A", "1")
	h.Set("x-auth-request-user", "u")
	h.Set("Cookie", "c")
	XoaHeader(h, xoaMacDinh)
	if len(h) != 1 || h.Get("Cookie") != "c" {
		t.Fatalf("%v", h)
	}
}

func TestHeaderGiaGachDuoiBiXoaQuaProxyThat(t *testing.T) {
	u := moiUpstream(t, nil)
	e := &hub.KetQua{TrangThai: hub.Active, MaGoi: "pro", Nguon: hub.NguonMang}
	s := dungProxy(t, u, choQua("cus_that", e), func(c *Cau) { c.Xoa = append(append([]string{}, xoaMacDinh...), "x-internal-*") })
	req, _ := http.NewRequest("GET", s.URL+"/x", nil)
	spoof := map[string]string{
		"X_DZ_Customer_Ref": "admin_1", "X-DZ_Plan": "enterprise", "x_dz_features": "everything", "X_Forwarded_User": "root",
		"X_Auth_Request_User": "root", "X_Auth_Request_Email": "r@x", "X_Internal_Secret": "s", "X-Internal_Other": "s",
	}
	for k, v := range spoof {
		req.Header.Set(k, v)
	}
	req.Header.Set("X-Giu-Nguyen", "ok")
	if _, err := http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	}
	got := u.cuoi(t).Header
	for k := range spoof {
		if got.Get(k) != "" {
			t.Errorf("%s lọt tới upstream: %q (Rack/PHP/WSGI sẽ đọc nó như dạng gạch ngang)", k, got.Get(k))
		}
	}
	if got.Get("X-Giu-Nguyen") != "ok" || got.Get("X-DZ-Customer-Ref") != "cus_that" || got.Get("X-DZ-Plan") != "pro" {
		t.Fatalf("header thường giữ, giá trị của gate: %v", got)
	}
}

func TestChuanHoaTenHeaderVaMauCungDuocChuanHoa(t *testing.T) {
	cacTruongHop := map[string]string{"X_DZ_Customer_Ref": "x-dz-customer-ref", "X-DZ_Plan": "x-dz-plan", "ABC": "abc", "a_b-c_d": "a-b-c-d"}
	for vao, mong := range cacTruongHop {
		if got := ChuanHoaTenHeader(vao); got != mong {
			t.Errorf("%q → %q, mong %q", vao, got, mong)
		}
	}
	h := http.Header{}
	h.Set("X_Foo_Bar", "1")
	h.Set("X-Keep", "2")
	XoaHeader(h, []string{"x_foo-*"}) // mẫu viết bằng gạch dưới cũng khớp (đã chuẩn hoá)
	if h.Get("X_Foo_Bar") != "" || h.Get("X-Keep") != "2" {
		t.Fatalf("%v", h)
	}
}

func TestXForwardedHostChiTuPublicOrigins(t *testing.T) {
	u := moiUpstream(t, nil)
	s := dungProxy(t, u, choQua("c", nil), func(c *Cau) {
		c.CongKhai = []Origin{{Scheme: "https", Host: "app.test"}, {Scheme: "https", Host: "www.app.test:8443"}}
	})
	cacTruongHop := []struct {
		ten, reqLine, host, mongHost string
	}{
		{"Host khớp origin đầu", "GET /x HTTP/1.1", "app.test", "app.test"},
		{"Host khớp origin sau (hoa/thường)", "GET /x HTTP/1.1", "WWW.App.Test:8443", "www.app.test:8443"},
		{"Host lạ ⇒ origin đầu", "GET /x HTTP/1.1", "evil.test", "app.test"},
		{"request-line tuyệt đối tới host lạ", "GET http://evil.test/x HTTP/1.1", "evil.test", "app.test"},
		{"khớp host nhưng sai port ⇒ origin đầu", "GET /x HTTP/1.1", "www.app.test", "app.test"},
	}
	for _, c := range cacTruongHop {
		_ = guiTho(t, strings.TrimPrefix(s.URL, "http://"), c.reqLine+"\r\nHost: "+c.host+"\r\nX-Forwarded-Host: ke-tan-cong.test\r\nX-Forwarded-Proto: gopher\r\nConnection: close\r\n\r\n")
		got := u.cuoi(t).Header
		if got.Get("X-Forwarded-Host") != c.mongHost || got.Get("X-Forwarded-Proto") != "https" {
			t.Errorf("%s: X-Forwarded-Host=%q Proto=%q, mong %q https", c.ten, got.Get("X-Forwarded-Host"), got.Get("X-Forwarded-Proto"), c.mongHost)
		}
	}
}

func TestVungDZTrenCongCongKhaiTra404KhongProxy(t *testing.T) {
	u := moiUpstream(t, nil)
	s := dungProxy(t, u, choQua("", nil), nil) // bypass ẩn danh: quyết định `allowed`
	for _, p := range []string{"/_dz/healthz", "/_dz/readyz", "/_dz/metrics", "/_dz", "/_dz/", "/_dz/x/y?a=1", "/%5Fdz/x", "/_dz/../x", "//_dz/healthz"} {
		got := guiTho(t, strings.TrimPrefix(s.URL, "http://"), "GET "+p+" HTTP/1.1\r\nHost: a\r\nConnection: close\r\n\r\n")
		if !strings.HasPrefix(got, "HTTP/1.1 404") || !strings.Contains(got, "Cache-Control: no-store") {
			t.Errorf("%s: %.80q", p, got)
		}
	}
	if u.soGoi() != 0 {
		t.Fatalf("/_dz/* không được chuyển tới app: %d lần", u.soGoi())
	}
	for _, p := range []string{"/_dzx", "/dz/x", "/a/_dz/x", "/api/dz/webhook"} {
		if got := guiTho(t, strings.TrimPrefix(s.URL, "http://"), "GET "+p+" HTTP/1.1\r\nHost: a\r\nConnection: close\r\n\r\n"); !strings.HasPrefix(got, "HTTP/1.1 200") {
			t.Errorf("%s phải tới app: %.60q", p, got)
		}
	}
}

func TestHeaderNguonGocCuaClientBiXoaTruocKhiGateTuDat(t *testing.T) {
	u := moiUpstream(t, nil)
	s := dungProxy(t, u, choQua("c", nil), func(c *Cau) { c.CongKhai = []Origin{{Scheme: "https", Host: "app.test"}} })
	req, _ := http.NewRequest("GET", s.URL+"/x", nil)
	gia := map[string]string{
		"Forwarded": "for=6.6.6.6;host=evil.test", "X-Forwarded-Port": "1337", "X-Forwarded-Prefix": "/evil", "X-Forwarded-Ssl": "on",
		"X-Forwarded-Scheme": "gopher", "X-Forwarded-Server": "evil", "X-Forwarded-Bat-Ky": "x", "X-Original-Forwarded-For": "6.6.6.6",
		"X-Real-IP": "6.6.6.6", "True-Client-IP": "6.6.6.6", "CF-Connecting-IP": "6.6.6.6", "X-Client-IP": "6.6.6.6",
		"X-Cluster-Client-IP": "6.6.6.6", "Fastly-Client-IP": "6.6.6.6", "X-Forwarded-For": "6.6.6.6", "X-Forwarded-Host": "evil.test", "X-Forwarded-Proto": "gopher",
	}
	for k, v := range gia {
		req.Header.Set(k, v)
	}
	req.Header.Set("X-Giu-Nguyen", "ok")
	if _, err := http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	}
	got := u.cuoi(t).Header
	for k := range gia {
		switch k {
		case "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto":
			continue // do GATE tự đặt (kiểm bên dưới)
		}
		if got.Get(k) != "" {
			t.Errorf("%s của client lọt tới upstream: %q", k, got.Get(k))
		}
	}
	// ...nhưng X-Forwarded-* do chính gate đặt (từ kết nối thật + public_origins) phải CÒN: xoá nhầm sau khi đặt là lỗi thứ tự.
	if got.Get("X-Forwarded-For") != "127.0.0.1" || got.Get("X-Forwarded-Host") != "app.test" || got.Get("X-Forwarded-Proto") != "https" {
		t.Fatalf("X-Forwarded-* của gate: %v", got)
	}
	if got.Get("X-Giu-Nguyen") != "ok" {
		t.Fatal("header thường giữ")
	}
}

func TestHeaderGachDuoiMacDinhBiBoHet(t *testing.T) {
	u := moiUpstream(t, nil)
	s := dungProxy(t, u, choQua("c", nil), nil)
	req, _ := http.NewRequest("GET", s.URL+"/x", nil)
	for k, v := range map[string]string{"X_Custom": "1", "X-App_Thing": "2", "Is_Admin": "true", "X_DZ_Customer_Ref": "admin"} {
		req.Header.Set(k, v)
	}
	req.Header.Set("X-Giu-Nguyen", "ok")
	_, _ = http.DefaultClient.Do(req)
	got := u.cuoi(t).Header
	for _, k := range []string{"X_Custom", "X-App_Thing", "Is_Admin", "X_DZ_Customer_Ref"} {
		if got.Get(k) != "" {
			t.Errorf("header gạch dưới %s lọt qua: %q", k, got.Get(k))
		}
	}
	for k := range got {
		if strings.Contains(k, "_") {
			t.Errorf("upstream nhận header có `_`: %q", k)
		}
	}
	if got.Get("X-Giu-Nguyen") != "ok" {
		t.Fatal("header thường giữ")
	}
}

func TestChoPhepHeaderGachDuoiVanXoaHeaderDanhTinh(t *testing.T) {
	u := moiUpstream(t, nil)
	s := dungProxy(t, u, choQua("cus_that", nil), func(c *Cau) { c.GiuGachDuoi = true })
	req, _ := http.NewRequest("GET", s.URL+"/x", nil)
	req.Header.Set("X_Custom", "1")
	req.Header.Set("X_DZ_Customer_Ref", "admin")
	req.Header.Set("X_Forwarded_User", "root")
	_, _ = http.DefaultClient.Do(req)
	got := u.cuoi(t).Header
	if got.Get("X_Custom") != "1" {
		t.Fatalf("khai cho phép ⇒ giữ X_Custom: %v", got)
	}
	if got.Get("X_DZ_Customer_Ref") != "" || got.Get("X_Forwarded_User") != "" || got.Get("X-DZ-Customer-Ref") != "cus_that" {
		t.Fatalf("header danh tính gạch dưới vẫn phải bị xoá, chỉ còn giá trị của gate: %v", got)
	}
}
