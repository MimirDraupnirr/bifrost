# Image Bifröst (docs/23 §9.2) : en général sur la seedbox, données en volume.
# Pas d'agent embarqué : le binaire est déjà sur la machine des données.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -tags noagents -ldflags "-s -w -X main.Version=${VERSION}" -o /bifrost .

FROM alpine:3.21
RUN apk add --no-cache mediainfo ca-certificates tzdata \
 && adduser -D -u 1000 bifrost && mkdir -p /config /data && chown bifrost /config
COPY --from=build /bifrost /usr/local/bin/bifrost
USER bifrost
ENV BIFROST_IN_DOCKER=1
VOLUME ["/config"]
EXPOSE 8790
# Hors loopback : la page exige un mot de passe local, défini au premier lancement.
CMD ["bifrost", "--no-browser", "--listen", "0.0.0.0:8790", "--config", "/config/config.json"]
