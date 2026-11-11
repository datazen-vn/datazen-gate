// dz-gate: cổng paywall độc lập (reverse proxy) đứng trước app có sẵn của dev. Xem `internal/chay` cho luồng chạy.
package main

import (
	"os"

	"github.com/datazen-vn/datazen-gate/internal/chay"
)

// phienBan được gán lúc build: -ldflags "-X main.phienBan=1.2.3".
var phienBan = "dev"

func main() {
	os.Exit(chay.Main(os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr, phienBan))
}
