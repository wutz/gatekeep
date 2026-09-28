.PHONY: all build web test smoke dev clean

all: web build

build:
	go build -ldflags "-X github.com/wutz/gatekeep/internal/server.Version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o bin/ ./cmd/...

web:
	cd web && npm install && npm run build

test:
	go test ./...
	cd web && npx tsc --noEmit

smoke:
	./scripts/smoke.sh

dev: build
	@test -f configs/gatekeep.yaml || cp configs/gatekeep.example.yaml configs/gatekeep.yaml
	./bin/gatekeepd -config configs/gatekeep.yaml

clean:
	rm -rf bin web/dist
