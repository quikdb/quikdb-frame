FROM node:22-alpine AS builder
WORKDIR /build
COPY services/web/package.json ./
RUN npm install
COPY services/web ./
RUN npm run build

FROM golang:1.24-alpine AS server
WORKDIR /src
COPY go.mod ./
COPY services/web ./services/web
RUN mkdir -p /out && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /out/fileserver ./services/web

FROM scratch AS runtime
COPY --from=server /out/fileserver /fileserver
COPY --from=builder /build/dist /static
EXPOSE 3000
ENTRYPOINT ["/fileserver"]
