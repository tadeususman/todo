# ---- build stage ----
FROM golang:1.26-alpine AS build
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /todo ./cmd/server

# ---- run stage ----
FROM alpine:3.22
WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

RUN addgroup -S app && adduser -S -G app -H -s /sbin/nologin app

COPY --from=build /todo ./todo
COPY web ./web

USER app
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["./todo"]
