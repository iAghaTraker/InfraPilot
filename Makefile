VERSION ?= 0.4.1
DIST ?= dist
BINARIES = infrapilot infrapilot-agent infrapilot-web

.PHONY: release clean
release: clean
	@set -eu; for arch in amd64 arm64; do \
		d="$(DIST)/linux-$$arch"; \
		mkdir -p "$$d/installer/systemd"; \
		for b in $(BINARIES); do \
			CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath \
				-ldflags "-s -w -X github.com/iAghaTraker/InfraPilot/pkg/version.Version=$(VERSION)" \
				-o "$$d/$$b" ./cmd/$$b; \
		done; \
		cp install.sh "$$d/install.sh"; \
		cp installer/install.sh "$$d/installer/install.sh"; \
		cp installer/config.sample.yaml "$$d/installer/config.sample.yaml"; \
		cp installer/systemd/*.service "$$d/installer/systemd/"; \
		tar -C "$(DIST)" -czf "$(DIST)/InfraPilot_linux_$$arch.tar.gz" "linux-$$arch"; \
	done
	@cd "$(DIST)" && sha256sum *.tar.gz > checksums.txt
clean:
	rm -rf "$(DIST)"
