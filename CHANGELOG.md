# Changelog

Theo [Keep a Changelog](https://keepachangelog.com/vi/1.1.0/); semver nghiêm (repo công khai từ 11/11/2026; `0.x` tới khi E2E lát 2 xanh).

## [Chưa phát hành]

### Thêm
- Workflow `phat-hanh.yml`: tag `v*` ⇒ build linux/darwin x amd64/arm64 (`-trimpath`, phiên bản qua ldflags), ghi `SHA256SUMS`, tạo GitHub release. Mục "Tải bản dựng" trong README và `docs/trien-khai.md`.
- `gate.trusted_proxies`: tin `X-Forwarded-For`/`-Proto` của load balancer đã khai (chỉ khi kết nối từ dải đó; mặc định rỗng = hành vi cũ). `--check` báo lỗi CIDR sai, cảnh báo `0.0.0.0/0`/`::/0`.
- Chế độ `gate.mode: forward-auth`: `GET /_dz/auth` cho Caddy `forward_auth`, Traefik ForwardAuth, nginx `auth_request`. Dựng request ảo từ `X-Forwarded-Method/-Host/-Uri` hoặc `X-Original-URI/-Method`, dùng đúng lõi quyết định của chế độ proxy (200 + `X-DZ-*` hoặc 302/401/402/503 cùng thân/`Retry-After`). Chỉ tin proxy trong `gate.forward_auth.allow_from` (403 `dz_forward_untrusted`, không gọi hub); thiếu/hỏng URI hoặc Host ngoài `paywall.public_origins` ⇒ 400 `dz_forward_bad_request`. Hướng dẫn: `docs/forward-auth.md`.

### Đổi
- Đổi tên: module Go `github.com/datazen-vn/market-gate` → `github.com/datazen-vn/datazen-gate` (tên binary `dz-gate` giữ nguyên).
- Ghim lại `gate-v1` theo meta main `f5728a0` (63 ca; schema.json: cổng pattern ≤ 255).
- Go 1.26.11 (go.mod + image build): sửa GO-2026-6859 ở `net/http` (trước đó 1.26.10 sửa GO-2026-6809 ở `crypto/x509`) mà govulncheck bắt.
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
- Chế độ forward-auth, tuỳ chọn tắt log đường dẫn.
