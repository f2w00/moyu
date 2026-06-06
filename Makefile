APP_NAME = moyu
REGISTRY = 10.15.22.234:5005
IMAGE = $(REGISTRY)/$(APP_NAME)
TAG = latest

.PHONY: build run once clean lint help docker-build docker-push

BINARY=moyu

build:
	CGO_ENABLED=0 go build -o $(BINARY) .

run:
	sudo ./$(BINARY)

once:
	sudo ./$(BINARY) -once

clean:
	rm -f $(BINARY) moyu.db

lint:
	go vet ./...

docker-build:
	docker build -t $(IMAGE):$(TAG) .
	@echo "Built: $(IMAGE):$(TAG)"

docker-push:
	docker push $(IMAGE):$(TAG)
	@echo "Pushed: $(IMAGE):$(TAG)"

help:
	@echo "build         - 编译二进制"
	@echo "run           - 编译并启动 (daemon + HTTP)"
	@echo "once          - 编译并执行单次扫描"
	@echo "clean         - 删除二进制和数据库"
	@echo "lint          - 运行 go vet"
	@echo "docker-build  - 构建 Docker 镜像"
	@echo "docker-push   - 推送 Docker 镜像"
