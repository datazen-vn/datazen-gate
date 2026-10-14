#!/usr/bin/env bash
# Cong: govulncheck — lo bao mat da biet MA CODE CO GOI TOI (thu vien chuan + phu thuoc).
#
# Kho nay la cong ra Internet (nhan request cua khach cuoi) nen lo net/http, crypto/tls, net/url cham thang vao duong request.
#
# MA THOAT: 0 khong co lo bi goi · 1 CO lo bi goi (govulncheck tra 3) · 2 KHONG KIEM DUOC
# (loi mang tai govulncheck / CSDL lo hong, hoac loi khac). Mot loi mang KHONG duoc ve thanh 0.
# Phien ban GHIM, khong `@latest`: cong doi ket qua theo ngay la cong khong tai lap duoc.
#   Doi ban: sua VERSION o day. CSDL lo (vuln.go.dev) van tuoi moi luot — dung y.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VERSION="v1.8.0"

# `go install` roi chay binary — KHONG `go run`: `go run` nuot ma thoat cua chuong trinh con
# (govulncheck thoat 3 => `go run` thoat 1 + in "exit status 3"), nen khong phan biet duoc
# "co lo" voi "loi tai cong cu". Do 03/10.
BIN_DIR="$(mktemp -d)" || exit 2
trap 'rm -rf "$BIN_DIR"' EXIT
GOBIN="$BIN_DIR" go install "golang.org/x/vuln/cmd/govulncheck@${VERSION}" || {
  echo "[MA 2] KHONG KIEM DUOC — khong tai/dung duoc govulncheck ${VERSION} (loi mang?). KHONG phai 'sach'."
  exit 2
}
ma=0
"$BIN_DIR/govulncheck" ./... || ma=$?

case "$ma" in
  0) echo "[XANH] govulncheck ${VERSION}: 0 lo bi goi."; exit 0 ;;
  3) echo "[DO] govulncheck ${VERSION}: CO lo ma code goi toi — xem tren. Nang Go/phu thuoc theo 'Fixed in'."; exit 1 ;;
  *) echo "[MA 2] KHONG KIEM DUOC — govulncheck thoat ${ma} (loi mang/tai cong cu/CSDL lo?). KHONG phai 'sach'."; exit 2 ;;
esac
