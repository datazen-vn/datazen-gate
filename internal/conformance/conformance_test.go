package conformance

import "testing"

// TestConformance chạy từng ca của bộ gate-v1 thành MỘT test con. Không đọc được bộ ca ⇒ đỏ (mã "KHÔNG KIỂM ĐƯỢC"), không bao giờ skip.
func TestConformance(t *testing.T) {
	cas, err := NapCa(ThuMucMeta())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("gate-v1: %d ca từ %s", len(cas), ThuMucMeta())
	for _, c := range cas {
		t.Run(c.ID, func(t *testing.T) { chayMotCa(t, c) })
	}
}

func TestKhongDocDuocBoCaLaLoi(t *testing.T) {
	if _, err := NapCa(t.TempDir()); err == nil {
		t.Fatal("thư mục meta rỗng phải là lỗi KHÔNG KIỂM ĐƯỢC, không phải 0 ca xanh")
	}
}
