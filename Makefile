.PHONY: dev test vet build up down seed-clean

# 本地开发（演示模式，无需模型）
dev:
	go run ./cmd/server

test:
	go test ./...

vet:
	go vet ./...

build:
	CGO_ENABLED=0 go build -o lulu-rpg ./cmd/server

up:
	docker compose up -d --build

down:
	docker compose down
