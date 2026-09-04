# Go 服务镜像(target server):单 module 构建出两个二进制;同一镜像按
# compose 的 command 分别作为 gateway/canvas 运行。
FROM golang:1.26-alpine AS server-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gateway-server ./gateway/server \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/canvas-server ./canvas/server

FROM alpine:3.22 AS server
RUN apk add --no-cache ca-certificates tzdata
COPY --from=server-build /out/ /app/
CMD ["/app/gateway-server"]

# --- 前端(16 号票):pnpm workspace 编出两个 SPA 静态产物,交给下方
# 两个 nginx 运行时托管;运行时容器只含构建产物,不依赖源码与 pnpm。 ---
FROM node:22-alpine AS web-build
WORKDIR /src
RUN npm install -g pnpm@8.15.9
# 先只拷贝清单文件装依赖,源码变更不再拖慢依赖层缓存。
COPY pnpm-lock.yaml pnpm-workspace.yaml package.json ./
COPY admin-web/package.json admin-web/
COPY canvas/web/package.json canvas/web/
COPY packages/api/package.json packages/api/
COPY packages/ui/package.json packages/ui/
RUN pnpm install --frozen-lockfile
COPY admin-web/ admin-web/
COPY canvas/web/ canvas/web/
COPY packages/ packages/
RUN pnpm --filter admin-web build \
 && pnpm --filter canvas-web build

# 两个前端运行时:nginx 托管 dist 并反代对应后端(反代规则与 dev 代理同形,
# 见 deploy/nginx/*.conf)。compose 以 target: admin-web / canvas-web 分别构建。
FROM nginx:1.28-alpine AS admin-web
COPY deploy/nginx/admin.conf /etc/nginx/conf.d/default.conf
COPY --from=web-build /src/admin-web/dist /usr/share/nginx/html

FROM nginx:1.28-alpine AS canvas-web
COPY deploy/nginx/canvas.conf /etc/nginx/conf.d/default.conf
COPY --from=web-build /src/canvas/web/dist /usr/share/nginx/html
