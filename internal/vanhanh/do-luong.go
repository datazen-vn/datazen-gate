package vanhanh

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

const soChuoiToiDa = 1000

// ThoiGianBien: các mốc (giây) của histogram thời gian xử lý một request (gồm cả upstream).
var thoiGianBien = []float64{0.005, 0.025, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// DoLuong là bộ đếm Prometheus tối giản (text exposition 0.0.4), tự viết để không kéo thêm phụ thuộc. Số chuỗi nhãn bị chặn
// (soChuoiToiDa): nhãn do người gọi liệt kê hữu hạn, nhưng một lỗi cài đặt cũng không được làm tràn bộ nhớ.
type DoLuong struct {
	mu     sync.Mutex
	dem    map[string]uint64
	tran   uint64
	hist   []uint64
	tong   float64
	soLuot uint64
}

// MoiDoLuong tạo bộ đếm rỗng.
func MoiDoLuong() *DoLuong {
	return &DoLuong{dem: map[string]uint64{}, hist: make([]uint64, len(thoiGianBien))}
}

func thoatNhan(v string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v)
}

// Cong tăng bộ đếm `ten` lên 1. `nhan` là cặp tên, giá trị: Cong("dz_gate_requests_total", "ket_qua", "allowed").
func (d *DoLuong) Cong(ten string, nhan ...string) {
	var sb strings.Builder
	sb.WriteString(ten)
	for i := 0; i+1 < len(nhan); i += 2 {
		if i == 0 {
			sb.WriteByte('{')
		} else {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `%s="%s"`, nhan[i], thoatNhan(nhan[i+1]))
	}
	if len(nhan) >= 2 {
		sb.WriteByte('}')
	}
	khoa := sb.String()
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, co := d.dem[khoa]; !co && len(d.dem) >= soChuoiToiDa {
		d.tran++
		return
	}
	d.dem[khoa]++
}

// ThoiGian ghi một lần đo (giây) vào histogram dz_gate_request_duration_seconds.
func (d *DoLuong) ThoiGian(giay float64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, b := range thoiGianBien {
		if giay <= b {
			d.hist[i]++
		}
	}
	d.tong += giay
	d.soLuot++
}

// Viet in toàn bộ số đo theo thứ tự ổn định.
func (d *DoLuong) Viet(w io.Writer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	khoa := make([]string, 0, len(d.dem))
	for k := range d.dem {
		khoa = append(khoa, k)
	}
	sort.Strings(khoa)
	daIn := map[string]bool{}
	for _, k := range khoa {
		ten := k
		if i := strings.IndexByte(k, '{'); i >= 0 {
			ten = k[:i]
		}
		if !daIn[ten] {
			daIn[ten] = true
			fmt.Fprintf(w, "# TYPE %s counter\n", ten)
		}
		fmt.Fprintf(w, "%s %d\n", k, d.dem[k])
	}
	fmt.Fprint(w, "# TYPE dz_gate_request_duration_seconds histogram\n")
	for i, b := range thoiGianBien {
		fmt.Fprintf(w, "dz_gate_request_duration_seconds_bucket{le=\"%g\"} %d\n", b, d.hist[i])
	}
	fmt.Fprintf(w, "dz_gate_request_duration_seconds_bucket{le=\"+Inf\"} %d\n", d.soLuot)
	fmt.Fprintf(w, "dz_gate_request_duration_seconds_sum %g\n", d.tong)
	fmt.Fprintf(w, "dz_gate_request_duration_seconds_count %d\n", d.soLuot)
	fmt.Fprint(w, "# TYPE dz_gate_metric_series_dropped_total counter\n")
	fmt.Fprintf(w, "dz_gate_metric_series_dropped_total %d\n", d.tran)
}
