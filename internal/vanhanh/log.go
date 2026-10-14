// Package vanhanh gom phần vận hành của dz-gate: log JSON có băm customer_ref, bộ đếm Prometheus tự viết, cổng /_dz/{healthz,readyz,metrics}
// trên bind riêng.
package vanhanh

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
)

// MoiLog: slog JSON ra `w` (mặc định stderr do người gọi chọn). KHÔNG bao giờ truyền cookie/token/khoá/query vào log.
func MoiLog(w io.Writer) *slog.Logger { return slog.New(slog.NewJSONHandler(w, nil)) }

// Bam băm customer_ref cho log: `sha256(salt ‖ ref)[:16 hex]`. Cùng ref ⇒ cùng giá trị trong một lần chạy (đủ để lần theo một khách), nhưng
// không đảo ngược được và KHÔNG so được giữa các lần chạy (salt ngẫu nhiên mỗi tiến trình): dev hay dùng email làm ref.
type Bam struct{ salt []byte }

// MoiBam: salt rỗng ⇒ ngẫu nhiên 16 byte.
func MoiBam(salt string) *Bam {
	if salt != "" {
		return &Bam{salt: []byte(salt)}
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("không đọc được nguồn ngẫu nhiên của hệ điều hành: " + err.Error())
	}
	return &Bam{salt: b}
}

// Khach trả 16 ký tự hex; ref rỗng ⇒ "".
func (b *Bam) Khach(ref string) string {
	if ref == "" {
		return ""
	}
	h := sha256.New()
	h.Write(b.salt)
	h.Write([]byte(ref))
	return hex.EncodeToString(h.Sum(nil))[:16]
}
