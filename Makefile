.PHONY: build run once clean lint help

BINARY=moyu

build:
	go build -o $(BINARY) .

run:
	sudo ./$(BINARY)

once:
	sudo ./$(BINARY) -once

clean:
	rm -f $(BINARY) arpscan.db

lint:
	go vet ./...

help:
	@echo "build  - 编译二进制"
	@echo "run    - 编译并启动 (daemon + HTTP)"
	@echo "once   - 编译并执行单次扫描"
	@echo "clean  - 删除二进制和数据库"
	@echo "lint   - 运行 go vet"
