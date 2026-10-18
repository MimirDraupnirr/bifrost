VERSION ?= dev
LDFLAGS  = -s -w -X main.Version=$(VERSION)

.PHONY: build agents test clean desktop-sidecar desktop

# Les agents Linux d'abord (sans embarquer d'agents), puis le binaire du poste qui les embarque.
agents:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags noagents -ldflags "$(LDFLAGS)" -o agent/bifrost-linux-amd64 .
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags noagents -ldflags "$(LDFLAGS)" -o agent/bifrost-linux-arm64 .

build: agents
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bifrost .

test:
	go vet ./... && go test ./...

# Application de bureau (desktop/) : le serveur Go en sidecar, nommé selon la cible de
# rustc (Tauri l'exige), puis le paquet de la plateforme (.app/.dmg, setup .exe, AppImage/.deb).
TRIPLE ?= $(shell rustc -vV | sed -n 's/^host: //p')
desktop-sidecar: agents
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o desktop/src-tauri/binaries/bifrost-$(TRIPLE)$(EXE) .

desktop: desktop-sidecar
	cd desktop && npm ci && npx tauri build

clean:
	rm -f bifrost agent/bifrost-linux-*
