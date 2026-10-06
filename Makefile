VERSION ?= dev
LDFLAGS  = -s -w -X main.Version=$(VERSION)

.PHONY: build agents test clean

# Les agents Linux d'abord (sans embarquer d'agents), puis le binaire du poste qui les embarque.
agents:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags noagents -ldflags "$(LDFLAGS)" -o agent/bifrost-linux-amd64 .
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags noagents -ldflags "$(LDFLAGS)" -o agent/bifrost-linux-arm64 .

build: agents
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bifrost .

test:
	go vet ./... && go test ./...

clean:
	rm -f bifrost agent/bifrost-linux-*
