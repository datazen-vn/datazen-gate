package cauhinh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const khoaThu = "dz_test_khoa-tu-tep-abc123"

func vietTep(t *testing.T, noiDung string, quyen os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "khoa")
	if err := os.WriteFile(p, []byte(noiDung), quyen); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, quyen); err != nil { // WriteFile chịu umask
		t.Fatal(err)
	}
	return p
}

func envTep(p string) Env { return envGia(map[string]string{"DZ_API_KEY_FILE": p}) }

func TestKhoaAPITuTep(t *testing.T) {
	c := docMau(t)
	for ten, tc := range map[string]struct {
		noiDung string
		quyen   os.FileMode
		canh    bool
	}{
		"0400":                   {khoaThu, 0o400, false},
		"0600":                   {khoaThu, 0o600, false},
		"xuống dòng cuối bị cắt": {"  " + khoaThu + "\r\n", 0o400, false},
		"0644 = cảnh báo":        {khoaThu, 0o644, true},
		"0440 = cảnh báo":        {khoaThu, 0o440, true},
	} {
		khoa, co, canh, err := c.KhoaAPI(envTep(vietTep(t, tc.noiDung, tc.quyen)))
		if err != nil || !co || khoa != khoaThu {
			t.Errorf("%s: %q %v %v", ten, khoa, co, err)
		}
		if (len(canh) > 0) != tc.canh {
			t.Errorf("%s: cảnh báo %v, mong có=%v", ten, canh, tc.canh)
		}
		for _, m := range canh {
			if strings.Contains(m, khoaThu) {
				t.Errorf("%s: cảnh báo lộ khoá: %s", ten, m)
			}
		}
	}
}

func TestKhoaAPITuTepQuaLienKetMem(t *testing.T) { // Kubernetes secrets là liên kết mềm (`..data`)
	thuc := vietTep(t, khoaThu, 0o400)
	lk := filepath.Join(t.TempDir(), "lk")
	if err := os.Symlink(thuc, lk); err != nil {
		t.Skip("không tạo được symlink:", err)
	}
	if khoa, co, _, err := docMau(t).KhoaAPI(envTep(lk)); err != nil || !co || khoa != khoaThu {
		t.Fatalf("%q %v %v", khoa, co, err)
	}
}

func TestKhoaAPITuTepLoi(t *testing.T) {
	c := docMau(t)
	thuMuc := t.TempDir()
	for ten, tc := range map[string]struct {
		env Env
		co  string // đoạn bắt buộc có trong lỗi
	}{
		"không tồn tại":    {envTep(filepath.Join(thuMuc, "khong-co")), "không mở được"},
		"là thư mục":       {envTep(thuMuc), "không phải tệp thường"},
		"rỗng":             {envTep(vietTep(t, "", 0o400)), "rỗng"},
		"chỉ khoảng trắng": {envTep(vietTep(t, " \n\t", 0o400)), "rỗng"},
		"quá lớn":          {envTep(vietTep(t, strings.Repeat("a", khoaTepToiDa+1), 0o400)), "lớn quá"},
		"cả biến và tệp":   {envGia(map[string]string{"DZ_API_KEY": khoaThu, "DZ_API_KEY_FILE": vietTep(t, khoaThu, 0o400)}), "chỉ dùng MỘT nguồn"},
	} {
		khoa, co, _, err := c.KhoaAPI(tc.env)
		if err == nil || co || khoa != "" || !strings.Contains(err.Error(), tc.co) {
			t.Errorf("%s: phải lỗi chứa %q: %q %v %v", ten, tc.co, khoa, co, err)
		}
	}
}

func TestKhoaAPIUuTienVaThieu(t *testing.T) {
	c := docMau(t)
	// Chỉ biến ⇒ như cũ.
	if khoa, co, canh, err := c.KhoaAPI(envGia(map[string]string{"DZ_API_KEY": khoaThu})); khoa != khoaThu || !co || canh != nil || err != nil {
		t.Fatalf("%q %v %v %v", khoa, co, canh, err)
	}
	// Không nguồn nào, hoặc _FILE rỗng ⇒ chưa đặt (không lỗi riêng: Kiem báo).
	for _, kv := range []map[string]string{{}, {"DZ_API_KEY_FILE": ""}, {"DZ_API_KEY_FILE": "  "}, {"DZ_API_KEY": " "}} {
		if khoa, co, _, err := c.KhoaAPI(envGia(kv)); khoa != "" || co || err != nil {
			t.Errorf("%v: %q %v %v", kv, khoa, co, err)
		}
	}
	// Tên biến theo api_key_env.
	c.Hub.APIKeyEnv = "KHOA_CUA_TOI"
	if khoa, co, _, err := c.KhoaAPI(envGia(map[string]string{"KHOA_CUA_TOI_FILE": vietTep(t, khoaThu, 0o400)})); khoa != khoaThu || !co || err != nil {
		t.Fatalf("%q %v %v", khoa, co, err)
	}
}

func TestKiemKhoaTuTep(t *testing.T) {
	c := docMau(t)
	// Hợp lệ qua tệp 0400, không cảnh báo về khoá.
	kq := Kiem(c, envGia(map[string]string{"DZ_API_KEY_FILE": vietTep(t, khoaThu, 0o400), "DZ_JWT_SECRET": strings.Repeat("s", 32)}))
	if !kq.HopLe() {
		t.Fatalf("khoá từ tệp phải hợp lệ: %v", kq.Cac)
	}
	for _, v := range kq.Cac {
		if v.Truong == "hub.api_key_env" {
			t.Errorf("không có vấn đề về khoá: %s", v)
		}
	}
	// Quyền rộng ⇒ cảnh báo, vẫn hợp lệ.
	kq = Kiem(c, envGia(map[string]string{"DZ_API_KEY_FILE": vietTep(t, khoaThu, 0o644), "DZ_JWT_SECRET": strings.Repeat("s", 32)}))
	if !kq.HopLe() || !contains(vanDeMuc(kq, MucCanhBao), "hub.api_key_env") {
		t.Fatalf("0644: cảnh báo, không lỗi: %v", kq.Cac)
	}
	// Nội dung tệp sai tiền tố ⇒ lỗi, KHÔNG lộ nội dung.
	bi := "mat-khau-nhay-cam-12345"
	kq = Kiem(c, envGia(map[string]string{"DZ_API_KEY_FILE": vietTep(t, bi, 0o400), "DZ_JWT_SECRET": strings.Repeat("s", 32)}))
	if kq.HopLe() {
		t.Fatal("tiền tố sai phải lỗi")
	}
	for _, v := range kq.Cac {
		if strings.Contains(v.String(), bi) {
			t.Errorf("lộ nội dung tệp: %s", v)
		}
	}
	// Thiếu cả hai: thông báo nhắc cả hai cách.
	kq = Kiem(c, envGia(map[string]string{"DZ_JWT_SECRET": strings.Repeat("s", 32)}))
	if kq.HopLe() || !strings.Contains(kq.Cac[0].NoiDung, "DZ_API_KEY_FILE") {
		t.Fatalf("%v", kq.Cac)
	}
}

func TestKhachDungKhoaTuTep(t *testing.T) {
	c := docMau(t)
	ch := c.Khach(envTep(vietTep(t, khoaThu+"\n", 0o400)), nil, nil, "ua", nil)
	if ch.APIKey != khoaThu {
		t.Fatalf("khách hub phải dùng khoá từ tệp: %q", ch.APIKey)
	}
}

func vanDeMuc(k *KetQuaKiem, muc string) []string {
	var ds []string
	for _, v := range k.Cac {
		if v.Muc == muc {
			ds = append(ds, v.Truong)
		}
	}
	return ds
}

func TestCanhBaoOpsListenKhongLoopback(t *testing.T) {
	for listen, canh := range map[string]bool{
		"127.0.0.1:9090": false, "localhost:9090": false, "[::1]:9090": false, "127.0.0.2:9090": false,
		"0.0.0.0:9090": true, ":9090": true, "[::]:9090": true, "10.0.0.5:9090": true, "ops.noi-bo:9090": true,
	} {
		c := docMau(t)
		c.Ops.Listen = listen
		kq := Kiem(c, envTest)
		if !kq.HopLe() {
			t.Fatalf("%s: chỉ là cảnh báo, không lỗi: %v", listen, kq.Cac)
		}
		if contains(vanDeMuc(kq, MucCanhBao), "ops.listen") != canh {
			t.Errorf("ops.listen %q: cảnh báo=%v, mong %v", listen, !canh, canh)
		}
	}
}

func TestCanhBaoSecretVietThang(t *testing.T) {
	bi := strings.Repeat("s", 32)
	c := docMau(t)
	if contains(vanDeMuc(Kiem(c, envTest), MucCanhBao), "identity[1].secret") {
		t.Fatal("mẫu không có secret viết thẳng, không được cảnh báo")
	}
	jwt := func(id Identity) Identity {
		id.Type, id.Issuer, id.Audience, id.Algorithms = "jwt", "https://app.vn", "app", []string{"HS256"}
		return id
	}
	// secret viết thẳng ⇒ cảnh báo (không lỗi, không lộ giá trị).
	c.Identity = append(c.Identity, jwt(Identity{Secret: bi}))
	kq := Kiem(c, envTest)
	if !kq.HopLe() || !contains(vanDeMuc(kq, MucCanhBao), "identity[2].secret") {
		t.Fatalf("secret viết thẳng: cảnh báo, không lỗi: %v", kq.Cac)
	}
	for _, v := range kq.Cac {
		if strings.Contains(v.String(), bi) {
			t.Errorf("cảnh báo lộ secret: %s", v)
		}
	}
	// secret_env ⇒ không cảnh báo.
	c = docMau(t)
	c.Identity = append(c.Identity, jwt(Identity{SecretEnv: "DZ_JWT_SECRET"}))
	if kq := Kiem(c, envTest); !kq.HopLe() || contains(vanDeMuc(kq, MucCanhBao), "identity[2].secret") {
		t.Fatalf("secret_env không được cảnh báo: %v", kq.Cac)
	}
}
