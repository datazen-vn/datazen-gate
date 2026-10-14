# market-gate (dz-gate)

Cổng paywall độc lập cho app có sẵn gắn thuê bao DataZen: đứng trước app của dev, nhận diện khách, hỏi hub quyền dùng, rồi cho qua / chuyển tới trang thanh toán. Không cần sửa code app.

Trạng thái: đang xây (repo private tới khi E2E lát 2 xanh).

| | PR #1 | PR #2 (đây) | PR #3 |
|---|---|---|---|
| Khớp tuyến, danh tính (JWT+JWKS, session hook), client hub, lõi quyết định + paywall, cấu hình + kiểm luật | có | | |
| Reverse proxy (xoá/tiêm header), `/_dz/healthz`/`readyz`/`metrics`, `cmd/dz-gate`, runner conformance (62 ca gate-v1) | | có | |
| Dockerfile, compose, tài liệu triển khai, `kiem-docker.sh` (forward-auth: chưa) | | | có (PR này) |

## Hành vi (tóm tắt)

- Request → bypass (`/api/dz/*`, `/_dz/*`, `login_url`, `success_path`) hoặc bảng `routes` (first-match, `allow | require | soft`) → danh tính → quyền dùng.
- Trình duyệt điều hướng (`GET`/`HEAD`, `Sec-Fetch-Mode: navigate`) chưa có quyền → `302` tới trang thanh toán; API/XHR/non-GET → `402` JSON (`checkout_url` vắng nếu không tạo được phiên).
- Chưa đăng nhập → `302` login / `401`. Hub lỗi → `503` (fail-closed) hoặc cho qua theo `fail_mode: open` của tuyến, chỉ với lỗi mạng/timeout/5xx.
- Danh tính KHÔNG xác định được (session_hook timeout/5xx, JWKS không tải được) trên tuyến cần quyền → `503` + `Retry-After: 5`, **không bao giờ** cho qua ẩn danh, kể cả `fail_mode: open` (kẻ tấn công tự gây được lỗi này). `fail_mode` chỉ áp cho hub lỗi.
- Khách đã `active` đúng gói mà thiếu feature của tuyến → `402` KHÔNG tạo phiên (hub sẽ 409). Hỏi lại sau 409 = đọc tươi chịu trần 1 lần / 5 s / khách, không xoá đệm quyền.
- Khách quay về `success_path?checkout_session_id=<uuid>` → đọc tươi quyền (tối đa 1 lần / 5 s / khách), không bị đệm "chưa có quyền" giữ lại.
- Hợp đồng đầy đủ: `../meta/contracts/conformance/gate-v1/README.md`.

## Triển khai an toàn (BẮT BUỘC đọc)

- **App của bạn chỉ được tới được QUA gate.** Upstream tin `X-DZ-*` (khách, gói, tính năng). Gate xoá mọi `X-DZ-*`, `X-Auth-Request-*`, `X-Forwarded-User` do client gửi rồi mới tiêm của mình, nhưng nếu app còn mở cổng ra ngoài song song (publish port, cổng LB khác, đường tắt nội bộ), kẻ tấn công gửi thẳng header giả vào app và gate không cứu được. Trong compose: `app` KHÔNG publish port, chỉ `dz-gate` publish; mạng nội bộ riêng.
- **Cổng vận hành (`ops.listen`) bind riêng** (mặc định `127.0.0.1:9090`), không đưa ra Internet.
- **`X-Forwarded-For` sau load balancer**: gate dựng `X-Forwarded-For`/`-Host`/`-Proto` từ KẾT NỐI TRỰC TIẾP, không tin giá trị client gửi — nên sau LB upstream thấy IP của LB. Follow-up (chưa làm): `trusted_proxies` để tin header của LB đã khai.

- **Header của client bị xoá/bỏ trước khi gate tự đặt**: ngoài `X-DZ-*`/`X-Auth-Request-*`/`X-Forwarded-User`, gate xoá `Forwarded`, `X-Forwarded-*` (mọi biến thể: -Port, -Prefix, -Ssl, -Scheme, -Server…), `X-Original-Forwarded-*`, `X-Real-IP`, `X-Client-IP`, `X-Cluster-Client-IP`, `True-Client-IP`, `CF-Connecting-IP`, `Fastly-Client-IP` (app tin chúng để dựng IP/URL/giới hạn tốc độ), rồi mới đặt `X-Forwarded-For/-Host/-Proto` từ kết nối thật. **Mọi header có `_` trong tên bị bỏ** (như nginx `underscores_in_headers off`); app thật sự cần thì khai `inject.allow_underscore_headers: true` (có cảnh báo; header danh tính gạch dưới vẫn bị xoá).
- **Tên header chuẩn hoá khi xoá**: hạ chữ thường VÀ `_` → `-` (server CGI-style như Rack/PHP/WSGI cũ đọc `X_DZ_Customer_Ref` như `X-DZ-Customer-Ref`); áp cho cả `strip_incoming`.
- **`X-Forwarded-Host`/`-Proto` chỉ theo `paywall.public_origins`**: Host của client khớp một origin thì dùng origin đó, không khớp thì dùng origin ĐẦU; scheme là của origin (gate nghe HTTP sau bộ kết thúc TLS). App nhiều domain phải khai đủ domain trong `public_origins` (cần cho `success_url` rồi). So khớp theo `host[:port]` chữ thường, nguyên văn.
- **`/_dz/*` ở mặt công khai ⇒ gate tự trả 404**, không chuyển tới app (cổng vận hành chỉ ở `ops.listen`). `ops.listen` mặc định `127.0.0.1`: nếu Prometheus ở container khác phải đổi sang địa chỉ mạng nội bộ — không bao giờ `0.0.0.0` trên mạng công cộng.
- **`Host` gửi upstream là host của upstream** (không phải Host gốc của client): app định tuyến theo vhost cần biết điều này; Host gốc đã được xác thực nằm ở `X-Forwarded-Host`.
- Thân request chậm: máy chủ có `ReadHeaderTimeout`/`MaxHeaderBytes` nhưng không `ReadTimeout` cho thân (proxy streaming/upload dài); chặn ở lớp trước nếu cần.

## Khai tuyến cho upstream Rails/Spring

`/premium/*` KHÔNG khớp `/premium.json`, nhưng Rails (`(.:format)`) và Spring < 5.3 (suffix pattern) định tuyến `/premium.json` về handler của `/premium`. Khai `/premium*` (hoặc cả `/premium` lẫn `/premium.*`), hoặc `default_action: require` rồi liệt kê phần công khai bằng `allow`.

## Tài liệu

[Triển khai](docs/trien-khai.md) · [Cấu hình](docs/cau-hinh.md) · [Phản hồi, mã lỗi, số đo](docs/ma-loi.md) · [Changelog](CHANGELOG.md) · mẫu: `deploy/gate.example.yaml`, `deploy/compose.example.yml`, `deploy/Dockerfile`.

## Phát triển

```bash
bash cong/kiem-kho.sh          # gofmt · vet · build · test -race (0 bỏ qua)
bash cong/kiem-conformance.sh  # 62 ca gate-v1 của meta, ghim SHA
bash cong/kiem-docker.sh       # build image thật + chạy + request qua cổng (cần docker)
bash cong/kiem-govulncheck.sh
```

Cấu hình mẫu: `deploy/gate.example.yaml` (= `internal/cauhinh/mau.go`, có test canh khớp). Xem `AGENTS.md` cho bố cục và ranh giới.
