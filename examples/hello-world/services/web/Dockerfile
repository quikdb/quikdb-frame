FROM node:22-alpine AS builder
WORKDIR /build
COPY package.json ./
RUN npm install
COPY . .
RUN npm run build

FROM golang:1.23-alpine AS server
WORKDIR /build
COPY server.go go.mod ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o fileserver .

FROM scratch
COPY --from=server /build/fileserver /fileserver
COPY --from=builder /build/dist /static
EXPOSE 3000
ENTRYPOINT ["/fileserver"]
