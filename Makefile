# Override with `make DOCKER=docker ...` when your user is in the docker group.
DOCKER ?= sudo docker

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.DEFAULT_GOAL := help
.PHONY: help image build web build-windows desktop test clean

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-15s %s\n", $$1, $$2}'

image: ## Build the Docker images knoxmgr and knoxmgr-mock, tagged $(VERSION) and latest
	$(DOCKER) build --build-arg VERSION=$(VERSION) --target knoxmgr -t knoxmgr:$(VERSION) -t knoxmgr:latest .
	$(DOCKER) build --build-arg VERSION=$(VERSION) --target knoxmock -t knoxmgr-mock:$(VERSION) -t knoxmgr-mock:latest .

web: ## Build the web UI into web/build
	cd web && npm ci && npm run build

build: web ## Build bin/knoxmgr and bin/knoxmock (linux)
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/ ./cmd/knoxmgr ./cmd/knoxmock

build-windows: web ## Build bin/*.exe for windows/amd64
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/ ./cmd/knoxmgr ./cmd/knoxmock

desktop: web ## Build dist/knoxmgr-desktop-windows-amd64.exe (portable WebView2 app)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$(VERSION)" -o dist/knoxmgr-desktop-windows-amd64.exe ./cmd/knoxmgr-desktop

test: ## go vet, go test and the web type check
	go vet ./...
	go test ./...
	cd web && npm run check

clean: ## Remove bin/ and dist/
	$(RM) -r bin dist
