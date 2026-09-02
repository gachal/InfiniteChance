# 单 module 构建出两个二进制;同一镜像按 compose 的 command 分别作为 gateway/canvas 运行。
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gateway-server ./gateway/server \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/canvas-server ./canvas/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/ /app/
CMD ["/app/gateway-server"]
