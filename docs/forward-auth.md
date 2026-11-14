# Chế độ forward-auth (Caddy, Traefik, nginx)

Đã có reverse proxy trước app? Đừng chèn thêm một hop: để proxy đó hỏi dz-gate trước mỗi request. Gate không có upstream, chỉ trả lời `GET /_dz/auth` ở cổng nghe chính (`gate.listen`); cổng vận hành (`ops.listen`) tách riêng như chế độ proxy.

```
khách ──▶ proxy của bạn ──(1) GET /_dz/auth + header gốc──▶ dz-gate ──▶ hub
                │  ◀── 200 + X-DZ-* (cho qua)  hoặc  302/401/402/503 (chặn, trả thẳng cho khách)
                └──(2) cho qua ⇒ chuyển tới app kèm X-DZ-*
```

## Cấu hình gate

```yaml
gate:
  listen: ":8080"
  mode: forward-auth
  forward_auth:
    allow_from: ["172.16.0.0/12"]   # CIDR của proxy (mạng docker/host); bắt buộc, không có loopback ngầm
paywall:
  public_origins: ["https://app.vn"] # bắt buộc: Host của request ảo phải khớp một origin
# hub, identity, routes, paywall.*: y hệt chế độ proxy
```

`X-Forwarded-Proto` không được dùng: scheme công khai lấy từ `paywall.public_origins`.

Quyết định giống hệt chế độ proxy (cùng lõi, cùng ca `gate-v1`): cho qua ⇒ `200` rỗng + `X-DZ-Customer-Ref`, `-Entitlement-Status`, `-Plan`, `-Features`, `-Grace-Until`, `-Entitlement-Source`; chặn ⇒ cùng `302`/`401`/`402`/`503` và thân JSON, `Retry-After` như [mã lỗi](ma-loi.md).

Gate dựng request ảo từ `X-Forwarded-Method`, `X-Forwarded-Host`, `X-Forwarded-Uri` (Caddy, Traefik) hoặc `X-Original-URI`, `X-Original-Method` (nginx); `Cookie`, `Authorization`, `Accept`, `Sec-Fetch-Mode` lấy từ chính request hỏi (proxy phải chép sang). Lỗi do gate tự trả, không gọi hub:

| HTTP | Khi nào |
|---|---|
| 403 `dz_forward_untrusted` | kết nối thật không thuộc `allow_from` (không bao giờ đọc `X-Forwarded-For`) |
| 400 `dz_forward_bad_request` | thiếu/hỏng URI (phải dạng `/đường?query`), thiếu Host, Host ngoài `public_origins`, header lặp |

Danh tính không xác định ⇒ `503` + `Retry-After`, không bao giờ cho qua.

## Caddy

```caddyfile
app.vn {
    route {
        # BẮT BUỘC: gate chỉ đặt header nào có giá trị (tuyến `allow` không có Customer-Ref), mà copy_headers không chắc xoá
        # giá trị client gửi ⇒ xoá trước, nếu không client giả được X-DZ-* ở tuyến công khai.
        request_header -X-DZ-Customer-Ref
        request_header -X-DZ-Entitlement-Status
        request_header -X-DZ-Plan
        request_header -X-DZ-Features
        request_header -X-DZ-Grace-Until
        request_header -X-DZ-Entitlement-Source
        forward_auth gate:8080 {
            uri /_dz/auth
            copy_headers X-DZ-Customer-Ref X-DZ-Entitlement-Status X-DZ-Plan X-DZ-Features X-DZ-Grace-Until X-DZ-Entitlement-Source
        }
        reverse_proxy app:3000
    }
}
```

Phản hồi không-2xx của gate (302/401/402/503) được Caddy trả nguyên cho khách.

## Traefik

```yaml
http:
  middlewares:
    dz-gate:
      forwardAuth:
        address: "http://gate:8080/_dz/auth"
        authResponseHeaders: [X-DZ-Customer-Ref, X-DZ-Entitlement-Status, X-DZ-Plan, X-DZ-Features, X-DZ-Grace-Until, X-DZ-Entitlement-Source]
        # không bật trustForwardHeader: Traefik tự đặt X-Forwarded-*
  routers:
    app: { rule: "Host(`app.vn`)", middlewares: [dz-gate], service: app }
```

## nginx

`auth_request` chỉ hiểu 2xx/401/403 (mã khác thành 500, mất 302/402 và thân JSON). Vì vậy request hỏi gửi `X-DZ-Auth-Mask: 1`: gate đổi phản hồi chặn thành `401` rỗng kèm `X-DZ-Auth-Status: <mã thật>`; nginx rồi hỏi lại gate (không có header che) để trả nguyên 302/402/503 cho khách. Quyền đã nằm trong đệm nên thường lần hỏi lại không tốn thêm lời gọi hub; riêng lỗi hub (503) không được đệm nên bị hỏi hub lần nữa.

```nginx
location = /_dz_auth {
    internal;
    proxy_pass http://gate:8080/_dz/auth;
    proxy_method GET;
    proxy_pass_request_body off;
    proxy_set_header Content-Length "";
    proxy_set_header X-Original-URI $request_uri;
    proxy_set_header X-Original-Method $request_method;
    proxy_set_header X-Forwarded-Host $host;
    proxy_set_header X-Forwarded-Uri "";       # xoá bản của client: gate trả 400 nếu thấy cả X-Forwarded-Uri/-Method lẫn X-Original-*
    proxy_set_header X-Forwarded-Method "";
    proxy_set_header X-DZ-Auth-Mask 1;
}

location @dz_verdict {                       # phản hồi thật (302/402/503) của gate
    proxy_pass http://gate:8080/_dz/auth;
    proxy_method GET;
    proxy_pass_request_body off;
    proxy_set_header Content-Length "";
    proxy_set_header X-Original-URI $request_uri;
    proxy_set_header X-Original-Method $request_method;
    proxy_set_header X-Forwarded-Host $host;
    proxy_set_header X-Forwarded-Uri "";       # xoá bản của client: gate trả 400 nếu thấy cả X-Forwarded-Uri/-Method lẫn X-Original-*
    proxy_set_header X-Forwarded-Method "";
}

location / {
    auth_request /_dz_auth;
    error_page 401 = @dz_verdict;
    auth_request_set $dz_ref    $upstream_http_x_dz_customer_ref;
    auth_request_set $dz_status $upstream_http_x_dz_entitlement_status;
    auth_request_set $dz_plan   $upstream_http_x_dz_plan;
    auth_request_set $dz_feat   $upstream_http_x_dz_features;
    auth_request_set $dz_grace  $upstream_http_x_dz_grace_until;
    auth_request_set $dz_src    $upstream_http_x_dz_entitlement_source;
    proxy_set_header X-DZ-Customer-Ref $dz_ref;           # đặt lại luôn: giá trị giả của client bị ghi đè/xoá
    proxy_set_header X-DZ-Entitlement-Status $dz_status;
    proxy_set_header X-DZ-Plan $dz_plan;
    proxy_set_header X-DZ-Features $dz_feat;
    proxy_set_header X-DZ-Grace-Until $dz_grace;
    proxy_set_header X-DZ-Entitlement-Source $dz_src;
    proxy_pass http://app:3000;
}
```

`X-DZ-Auth-Mask` chỉ có tác dụng khi request không có `X-Forwarded-Method` (tức nginx). Hiếm khi quyền đổi giữa hai lần hỏi, khách thấy một trang rỗng; tải lại là xong.

## Bắt buộc

- **App chỉ được tới qua proxy** (mạng nội bộ, không publish cổng của app): nếu không, ai cũng gửi được `X-DZ-*` giả.
- **Proxy phải xoá `X-DZ-*` của client** trước khi gắn header của gate (Caddy: `request_header -X-DZ-…` trong đoạn trên, vì gate không đặt header không có giá trị và `copy_headers` chỉ chép header có trong phản hồi; Traefik `authResponseHeaders` và nginx `proxy_set_header` ở trên đã đặt lại/xoá đủ sáu header). Header `X-DZ-*` khác sáu tên trên vẫn lọt tới app: app chỉ nên đọc sáu tên này. Kiểm: gửi `X-DZ-Customer-Ref: gia` tới tuyến `allow`; app không được thấy giá trị đó.
- `allow_from` chỉ gồm mạng của proxy; cổng `gate.listen` không publish ra ngoài.

## Đã kiểm

Test Go (`internal/proxy/forward-auth_test.go`, `internal/chay/forward-auth_test.go`) mô phỏng đúng bộ header Traefik, Caddy, nginx. Chưa chạy thử với binary Caddy/Traefik/nginx thật trong repo này; hãy dán cấu hình trên vào môi trường của bạn và kiểm bằng ca "Header giả" ở trên.
