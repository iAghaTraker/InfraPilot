VERSION ?= 0.4.1
DIST ?= dist
BINARIES = infrapilot infrapilot-agent infrapilot-web

.PHONY: release clean
release: clean
	@set -eu; for arch in amd64 arm64; do d="$(DIST)/linux-$$arch"; mkdir -p "$$d/installer/systemd"; for b in $(BINARIES); do CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath -ldflags "-s -w -X github.com/iAghaTraker/InfraPilot/pkg/version.Version=$(VERSION)" -o "$$d/$$b" ./cmd/$$b; done; cp installer/install.sh installer/config.sample.yaml install.sh "$$d/"; cp installer/systemd/*.service "$$d/installer/systemd/"; tar -C "$(DIST)" -czf "$(DIST)/InfraPilot_linux_$$arch.tar.gz" "linux-$$arch"; done
	@sha256sum $(DIST)/*.tar.gz > $(DIST)/checksums.txt
clean:
	rm -rf "$(DIST)"
