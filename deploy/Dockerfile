# dz-gate: image tối giản (distroless, chạy nonroot). Build từ GỐC kho:
#   docker build -f deploy/Dockerfile --build-arg PHIEN_BAN=0.1.0 -t dz-gate:0.1.0 .
#
# - CGO tắt, tệp tĩnh: không có shell, không có package manager trong image cuối (bề mặt tấn công nhỏ).
# - Không có shell nên HEALTHCHECK dùng chính binary (`--healthcheck`), dạng exec.
# - Base image ghim theo DIGEST (build tái lập, tag bị đẩy đè không đổi được image). Nâng cấp: `docker buildx imagetools inspect <tag> --format
#   '{{.Manifest.Digest}}'` rồi đổi digest (Renovate/Dependabot làm được tự động).
# - Chạy nonroot (uid 65532); cổng 8080 (khách) và 9090 (vận hành, KHÔNG publish ra ngoài).
FROM golang:1.26.9-alpine@sha256:3082400e369fa24d5fc60bca20edab3f6d604e0c5a690ec66b295eff4dd87ade AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG PHIEN_BAN=dev
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.phienBan=${PHIEN_BAN}" -o /out/dz-gate ./cmd/dz-gate

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/dz-gate /dz-gate
EXPOSE 8080 9090
USER nonroot:nonroot
ENTRYPOINT ["/dz-gate"]
CMD ["--config", "/etc/dz-gate/gate.yaml"]
HEALTHCHECK --interval=10s --timeout=4s --start-period=5s --retries=3 \
  CMD ["/dz-gate", "--healthcheck", "--config", "/etc/dz-gate/gate.yaml"]
