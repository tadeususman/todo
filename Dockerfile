# ---- build stage ----
FROM golang:1.23-alpine AS build
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /todo ./cmd/server

# ---- run stage ----
FROM alpine:3.20
WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY --from=build /todo ./todo
COPY web ./web

EXPOSE 8080
ENTRYPOINT ["./todo"]
