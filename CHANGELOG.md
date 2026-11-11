# Changelog

Theo [Keep a Changelog](https://keepachangelog.com/vi/1.1.0/); semver nghiêm (repo công khai từ 11/11/2026; `0.x` tới khi E2E lát 2 xanh).

## [Chưa phát hành]

### Đổi
- Go 1.26.10 (go.mod + image build): sửa GO-2026-6809 ở `crypto/x509` mà govulncheck bắt.
- CI: bỏ job `lint-kho` gọi action của repo meta private (repo public không gọi được); lint kho chạy ở máy.
- Tên gói SDK mặt tiền đổi sang `@datazen-vn/sdk`.
- Giấy phép MIT (trước là độc quyền), repo chuyển public; báo lỗ hổng qua private vulnerability reporting của GitHub.

## [0.1.0] — chưa phát hành

Cổng paywall độc lập (reverse proxy Go) cho app có sẵn gắn thuê bao DataZen. Hành vi dùng chung với `@datazen-vn/sdk/gate` qua bộ ca conformance `gate-v1` (meta).

### Thêm
- Lõi: khớp tuyến (glob first-match, bốn dạng đường dẫn, bypass), danh tính (JWT HS*/RS*/PS*/ES*/EdDSA + JWKS, session hook chống SSRF), client hub (đệm, ETag/304, stale-if-error, gộp lời gọi, trần đọc tươi 1 lần/5 s, phiên thanh toán đệm tới `replace_after`), quyết định + paywall 302/401/402/503/508.
- Proxy: xoá header danh tính/quyền/nguồn gốc kết nối của client (tên chuẩn hoá `_`→`-`), bỏ header gạch dưới, tiêm `X-DZ-*`, `X-Forwarded-*` theo `public_origins`, giữ đường dẫn thô, WebSocket, 502 khi upstream chết, `/_dz/*` công khai ⇒ 404.
- Vận hành: log JSON băm `customer_ref`, Prometheus, `/_dz/{healthz,readyz,metrics}` trên bind riêng, `--check`/`--healthcheck`/`--version`, tắt êm.
- Đóng gói: `deploy/Dockerfile` (distroless nonroot, ≈ 9 MB, healthcheck exec), `deploy/compose.example.yml` (app không publish cổng, gate chỉ publish `127.0.0.1:8080`, `mem_limit`/`pids_limit`, mạng nội bộ), base image ghim digest, `deploy/gate.example.yaml`, `docs/{trien-khai,cau-hinh,ma-loi}.md`, cổng `cong/kiem-docker.sh`.
- Cổng kho: `cong/kiem-kho.sh`, `cong/kiem-conformance.sh` (63 ca gate-v1, ghim SHA meta), `cong/kiem-govulncheck.sh`.

### Bảo mật
- `--check` cảnh báo `secret` viết thẳng trong YAML và `ops.listen` không phải loopback. Khoá API đọc được từ tệp (`DZ_API_KEY_FILE`, tệp thường ≤ 4 KiB, cảnh báo nếu quyền rộng hơn 0600; đặt cả biến và tệp là lỗi).
- Danh tính không xác định ⇒ 503 + `Retry-After`, không bao giờ fail-open; hỏi lại sau 409 chịu trần, không xoá đệm quyền (không vòng đốt hạn mức khoá API); đệm âm JWKS.

### Chưa có
- Chế độ forward-auth, `trusted_proxies`, tuỳ chọn tắt log đường dẫn.
