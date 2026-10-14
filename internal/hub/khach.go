package hub

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/datazen-vn/market-gate/internal/danhtinh"
	"github.com/datazen-vn/market-gate/internal/dem"
)

const (
	// TTLToiDa: trần cứng của cache_ttl (thu hồi quyền không được trễ quá 10 phút).
	TTLToiDa = 10 * time.Minute
	// TTLMacDinh của snapshot có quyền.
	TTLMacDinh = 60 * time.Second
	// KhongQuyenToiDa: snapshot KHÔNG có quyền (none/expired/cancelled/unknown) chỉ đệm tối đa chừng này, không phải trọn TTL:
	// khách vừa trả tiền không được kẹt ở "chưa có quyền" cả phút.
	KhongQuyenToiDa = 15 * time.Second
	// TranDocTuoi: mỗi customer_ref chỉ được MỘT lần đọc tươi trong khoảng này; quá trần thì dùng bản đệm.
	TranDocTuoi = 5 * time.Second
	// StaleToiDa: trần của stale_if_error, cũng là thời gian giữ mục quá hạn để hỏi lại bằng ETag.
	StaleToiDa    = time.Hour
	soMucDemToiDa = 10_000
)

// CauHinh của Khach. Giá trị 0 dùng mặc định an toàn.
type CauHinh struct {
	BaseURL string
	APIKey  string
	// HTTP: client nền (dùng lại Transport; luôn bị ép không theo chuyển hướng). nil ⇒ mặc định.
	HTTP         *http.Client
	Timeout      time.Duration // mặc định 5s
	TTL          time.Duration // 0 = tắt đệm; mặc định chỉ áp khi dùng DungMacDinh (xem Moi)
	StaleIfError time.Duration // 0 = tắt; cần TTL > 0
	CheDoLoi     string        // "closed" (mặc định) | "open"
	UserAgent    string
	Now          func() time.Time
	// OnLoi: gọi khi một lỗi bị cho qua (stale/fail-open) hoặc hub trả status lạ — để log/metric. Nên nhanh, không chặn.
	OnLoi func(*Loi)
}

// YeuCau của một lần Lay.
type YeuCau struct {
	TinhNang string // khoá trong features/limits ⇒ KetQua.TinhNang
	CheDoLoi string // ghi đè CheDoLoi của client cho lời gọi này
	// Tuoi: bỏ qua bản đệm tươi, đọc hub (khách vừa QUAY VỀ sau thanh toán). Tối đa 1 lần / 5 giây / khách.
	Tuoi bool
}

type luotBay struct {
	xong chan struct{}
	snap *Snapshot
	err  error
	tuoi bool
}

// Khach đọc quyền dùng của khách cuối từ hub, có đệm. An toàn cho nhiều goroutine.
type Khach struct {
	cfg     CauHinh
	http    *http.Client
	dem     *dem.Dem[*Snapshot]
	docTuoi *dem.Dem[struct{}]
	mu      sync.Mutex
	dangBay map[string]*luotBay
	now     func() time.Time
}

// Moi tạo Khach, điền mặc định (Timeout 5s, CheDoLoi closed, UserAgent). TTL KHÔNG được điền ngầm: 0 = tắt đệm.
func Moi(cfg CauHinh) *Khach {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.CheDoLoi == "" {
		cfg.CheDoLoi = "closed"
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "dz-gate"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Khach{
		cfg:     cfg,
		http:    clientKhongTheoChuyenHuong(cfg.HTTP),
		dem:     dem.Moi[*Snapshot](soMucDemToiDa, StaleToiDa, cfg.Now),
		docTuoi: dem.Moi[struct{}](soMucDemToiDa, 0, cfg.Now),
		dangBay: map[string]*luotBay{},
		now:     cfg.Now,
	}
}

func (k *Khach) baoLoi(l *Loi) {
	if k.cfg.OnLoi != nil {
		k.cfg.OnLoi(l)
	}
}

// Lay trả quyền dùng của khách. FAIL-CLOSED mặc định: không hỏi được hub ⇒ trả *Loi, không tự cho qua. CheDoLoi "open" chỉ nuốt
// sự cố hạ tầng (mạng/timeout/5xx) và đánh dấu Nguon = fail-open; sai khoá/hợp đồng/429 luôn trả lỗi.
func (k *Khach) Lay(ctx context.Context, maKhach string, y YeuCau) (*KetQua, error) {
	if !danhtinh.MaKhachHopLe(maKhach) {
		return nil, loi(LoiMaKhach, "customer_ref sai hình dạng")
	}
	cheDo := y.CheDoLoi
	if cheDo == "" {
		cheDo = k.cfg.CheDoLoi
	}
	muc, coMuc := k.dem.Lay(maKhach)
	bayGio := k.now()
	tuoi := coMuc && bayGio.Before(muc.TuoiDen)

	// Đọc tươi: bỏ bản đệm còn hạn. Tần suất bị TRẦN vì cờ này suy ra từ URL do client điều khiển — không để nó thành nút
	// "đập hub mỗi request". MỌI lần gọi hub vì tham số đều đặt mốc, kể cả khi đệm trống (README gate-v1 "Trần").
	// Quyết định + đặt mốc trần + đăng ký lần bay phải NGUYÊN TỬ (cùng một lần giữ khoá): nếu tách ra, yêu cầu quay về đồng thời
	// thấy mốc đã đặt mà chưa có lần bay để nhập vào, rồi dùng bản đệm "chưa có quyền".
	k.mu.Lock()
	thamGiaTuoi := false
	if b, ok := k.dangBay[maKhach]; ok && y.Tuoi && b.tuoi {
		thamGiaTuoi = true
	}
	bypass := false
	if y.Tuoi {
		_, daDoc := k.docTuoi.Lay(maKhach)
		bypass = thamGiaTuoi || !daDoc
	}
	if tuoi && !bypass {
		k.mu.Unlock()
		return chieu(muc.GiaTri, y.TinhNang, bayGio, NguonDem, nil), nil
	}
	if bypass && !thamGiaTuoi {
		k.docTuoi.Dat(maKhach, struct{}{}, bayGio.Add(TranDocTuoi))
	}
	luot, laChu := k.dangKy(maKhach, bypass)
	k.mu.Unlock()

	var truoc *Snapshot
	if coMuc {
		truoc = muc.GiaTri
	}
	snap, err := k.chay(ctx, maKhach, truoc, luot, laChu)
	if err == nil {
		return chieu(snap, y.TinhNang, k.now(), NguonMang, nil), nil
	}
	l, ok := ALoi(err)
	if !ok || !l.HaTang() {
		return nil, err
	}
	if cu := k.cuDuPhong(maKhach, bypass); cu != nil {
		k.baoLoi(l)
		return chieu(cu, y.TinhNang, k.now(), NguonCu, l), nil
	}
	if cheDo == "open" {
		k.baoLoi(l)
		return moCua(maKhach, y.TinhNang, l), nil
	}
	return nil, err
}

// Huy bỏ bản đệm của khách (vd khi nhận webhook đổi trạng thái), cả ghi-đệm của lời gọi đang bay và bản dự phòng stale, để câu
// trả lời cũ không sống lại sau khi webhook báo đổi.
func (k *Khach) Huy(maKhach string) {
	k.dem.Xoa(maKhach)
	k.mu.Lock()
	delete(k.dangBay, maKhach)
	k.mu.Unlock()
}
