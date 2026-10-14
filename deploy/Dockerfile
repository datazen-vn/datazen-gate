# dz-gate: image tối giản (distroless, chạy nonroot). Build từ GỐC kho:
#   docker build -f deploy/Dockerfile --build-arg PHIEN_BAN=0.1.0 -t dz-gate:0.1.0 .
#
# - CGO tắt, tệp tĩnh: không có shell, không có package manager trong image cuối (bề mặt tấn công nhỏ).
# - Không có shell nên HEALTHCHECK dùng chính binary (`--healthcheck`), dạng exec.
# - Chạy nonroot (uid 65532); cổng 8080 (khách) và 9090 (vận hành, KHÔNG publish ra ngoài).
FROM golang:1.26.9-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG PHIEN_BAN=dev
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.phienBan=${PHIEN_BAN}" -o /out/dz-gate ./cmd/dz-gate

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dz-gate /dz-gate
EXPOSE 8080 9090
USER nonroot:nonroot
ENTRYPOINT ["/dz-gate"]
CMD ["--config", "/etc/dz-gate/gate.yaml"]
HEALTHCHECK --interval=10s --timeout=4s --start-period=5s --retries=3 \
  CMD ["/dz-gate", "--healthcheck", "--config", "/etc/dz-gate/gate.yaml"]
