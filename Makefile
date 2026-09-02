.PHONY: up down logs ps test lint build dev-admin dev-canvas

up: ## 拉起全栈(MySQL、Redis、gateway、canvas)
	docker compose up -d --build

down: ## 停止并移除容器(数据卷保留)
	docker compose down

logs: ## 跟随全部服务日志
	docker compose logs -f

ps: ## 查看服务状态
	docker compose ps

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
