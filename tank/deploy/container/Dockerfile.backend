FROM golang:1.26-alpine@sha256:3082400e369fa24d5fc60bca20edab3f6d604e0c5a690ec66b295eff4dd87ade AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY cmd/ cmd/
COPY internal/ internal/
COPY pkg/ pkg/
RUN CGO_ENABLED=0 go build -trimpath -o /out/ ./cmd/coordinator ./cmd/tank-node ./cmd/tank-access ./cmd/tank-backup

FROM alpine:3.23@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0 AS runtime
RUN apk add --no-cache ca-certificates \
    && addgroup -g 10001 tank && adduser -D -u 10001 -G tank tank \
    && mkdir -p /data && chown tank:tank /data && chmod 0700 /data
COPY --from=build /out/ /usr/local/bin/
USER 10001:10001
WORKDIR /data
CMD ["coordinator"]
