FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY services/api ./services/api
RUN mkdir -p /out && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /out/app ./services/api

FROM scratch AS runtime
COPY --from=builder /out/app /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
EXPOSE 8080
ENTRYPOINT ["/app"]
