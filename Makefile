.PHONY: up down logs ps backup restore test lint build dev-admin dev-canvas

up: ## 拉起全栈(MySQL、Redis、gateway、canvas、两个前端)
	docker compose up -d --build

down: ## 停止并移除容器(数据卷保留)
	docker compose down

logs: ## 跟随全部服务日志
	docker compose logs -f

ps: ## 查看服务状态
	docker compose ps

backup: ## 备份全栈数据(MySQL/Redis/素材卷)→ backups/<时间戳>/
	deploy/backup.sh

restore: ## 恢复备份:make restore DIR=backups/<时间戳>(追加 Y=1 跳过确认)
	@[ -n "$(DIR)" ] || { echo '用法: make restore DIR=backups/<时间戳>(追加 Y=1 跳过确认)'; exit 1; }
	deploy/restore.sh $(DIR) $(if $(Y),-y,)

test: test-go test-web ## 跑全部测试与 vet

lint: lint-go lint-web ## 跑全部 lint

build: build-web ## 构建前端产物(Go 用 go build ./...)

test-go:
	go vet ./... && go test ./...

lint-go:
	go vet ./...

test-web:
	pnpm -r test

lint-web:
	pnpm -r lint

build-web:
	pnpm -r build

dev-admin: ## 启动管理后台 dev 服务器(:5173)
	pnpm --filter admin-web dev

dev-canvas: ## 启动画布前端 dev 服务器(:5174)
	pnpm --filter canvas-web dev

desktop-frontend: ## 构建两个 SPA 并复制进桌面内嵌目录(desktop/web/*)
	pnpm --filter admin-web build
	pnpm --filter canvas-web build
	rsync -a --delete --exclude='.gitkeep' admin-web/dist/ desktop/web/admin/
	rsync -a --delete --exclude='.gitkeep' canvas/web/dist/ desktop/web/canvas/

desktop: desktop-frontend ## 构建桌面应用(先自动构建 SPA)→ desktop/build/bin/
	go build -trimpath -ldflags="-s -w" -o desktop/build/bin/InfiniteChance ./desktop

dev-desktop: ## 跑桌面壳(配合 INFINITECHANCE_DEV=1 可指向 vite dev server)
	go run ./desktop
