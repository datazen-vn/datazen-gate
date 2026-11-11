# Phản hồi, mã lỗi, số đo và mã thoát của dz-gate

## Phản hồi do gate tự trả

Mọi phản hồi của gate đều `Cache-Control: no-store`; thân JSON `{ "code": "...", "status": N }` (HEAD không có thân). Phản hồi của upstream không bao giờ bị sửa.

| HTTP | `code` | Khi nào | Ghi chú |
|---|---|---|---|
| 302 | — | chưa đăng nhập + điều hướng trình duyệt + có `login_url` ⇒ `Location: login_url`; hoặc chưa có quyền + điều hướng + tạo được phiên ⇒ trang thanh toán | **không bao giờ** 302 cho non-GET/HEAD, cũng không cho fetch/XHR (`Sec-Fetch-Mode` ≠ `navigate`, `X-Requested-With`) |
| 401 | `dz_no_identity` | không nhận diện được khách | |
| 402 | `dz_not_entitled` | không có quyền / thiếu feature | thân: `status` (trạng thái thuê bao), `plan_code`, `checkout_url` (VẮNG nếu không tạo được phiên, kể cả khi khách đã active đúng gói), `grace_until` (hoặc `null`) |
| 400 | `dz_forward_bad_request` | chỉ `forward-auth`: thiếu/hỏng `X-Forwarded-Uri`/`X-Original-URI`, hoặc Host không khớp `paywall.public_origins` | không gọi hub |
| 403 | `dz_forward_untrusted` | chỉ `forward-auth`: kết nối không thuộc `gate.forward_auth.allow_from` | không gọi hub |
| 404 | `dz_not_found` | `/_dz/*` ở cổng công khai | cổng vận hành chỉ ở `ops.listen` |
| 502 | `dz_upstream_unavailable` | upstream không trả lời | không kèm chi tiết (tránh lộ địa chỉ nội bộ); dial tới app chết hết hạn sau 5 s |
| 503 | `dz_hub_unavailable` | không hỏi được hub (fail-closed), hub trả trạng thái lạ, 401/403/404/429 của hub (kể cả tuyến `fail_mode: open`) | |
| 503 | `dz_identity_unavailable` | danh tính không xác định (JWKS/hook lỗi) | kèm `Retry-After: 5`; không bao giờ fail-open trên tuyến cần quyền |
| 508 | `dz_gate_loop` | request đến đã mang `X-DZ-Gate-Hop` (gate proxy chính nó) | kiểm TRƯỚC mọi việc khác, kể cả tuyến bypass |

## Mã lỗi của client hub (log, số đo)

`dz_network_error`, `dz_timeout`, `dz_server_error` (5xx) là lỗi **hạ tầng**: chỉ loại này được `stale_if_error` và `fail_mode: open` cứu. Còn lại không bao giờ được cứu: `dz_service_key_invalid` (401), `dz_rate_limited` (429), `dz_bad_response` (sai hợp đồng, snapshot sai `customer_ref`, thân > 256 KB), `dz_redirect_refused` (3xx: cấu hình sai `base_url`), `dz_bad_request`, `dz_unexpected_status`, `dz_checkout_failed` (tạo/đọc phiên bị hub từ chối; kèm `hub_code` kebab-case như `thue-bao-dang-hieu-luc`, `phien-da-nhan-tien`, `plan-khong-ton-tai`), `dz_config_invalid`, `dz_invalid_customer_ref`.

## Số đo (`/_dz/metrics`)

| Số đo | Nhãn |
|---|---|
| `dz_gate_requests_total` | `ket_qua`: `allowed` · `no_identity` · `not_entitled` · `unavailable` · `loop` |
| `dz_gate_request_duration_seconds` | histogram (gồm cả thời gian upstream) |
| `dz_gate_errors_total` | `nguon` (`hub`/`danh_tinh`), `ma` (mã lỗi hub) |
| `dz_gate_upstream_errors_total` | — |
| `dz_gate_metric_series_dropped_total` | số chuỗi nhãn vượt trần 1000 |

## Log (JSON, stderr)

Một dòng `request` mỗi request: `method`, `path` (THÔ, bỏ query, ≤ 200 ký tự), `ket_qua`, `ms`, `status` (khi gate tự trả), `nguon` (nguồn quyền), `cref_hash`. **Không** log cookie, token, khoá, query, `customer_ref` thô. Lưu ý: đường dẫn dạng `/invite/<token>` vẫn vào log.

## Mã thoát

| Lệnh | 0 | 1 | 2 |
|---|---|---|---|
| `--check` | hợp lệ (in số cảnh báo) | cấu hình sai (đã liệt kê hết) | không đọc được tệp |
| chạy | tắt êm (SIGINT/SIGTERM, tối đa 10 s) | cấu hình sai · khoá API bị hub từ chối lúc khởi động · máy chủ dừng bất ngờ | — |
| `--healthcheck` | `/_dz/healthz` trả 200 | không trả lời/khác 200 | — |
