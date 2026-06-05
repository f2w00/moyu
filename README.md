# 📡 摸鱼雷达

基于 ARP 的网络设备在线监测工具，扫描局域网设备并记录在线时长，提供 Web 可视化面板。

## 功能

- ARP 扫描局域网设备（支持重试/退避）
- 5 分钟轮询，记录 06:00~24:00 的在线数据
- Web 面板展示今日详情/月度报告
- 用户别名管理（MAC→姓名映射）
- 自动清理半年前数据
- 每月 1 号自动生成 Markdown 月度报告
- `-once` 单次扫描模式

## 使用

### 编译

```bash
make build
```

### 运行（需要 root）

```bash
make run
```

### 单次扫描

```bash
make once
```

访问 `http://<设备IP>:9527`

## 参数

扫描超时等参数在 `main.go` 顶部常量配置。

## 技术栈

- Go 标准库 + net/http
- ARP: mdlayher/arp
- SQLite: modernc.org/sqlite
- 前端: 纯 HTML/CSS/JS 单页应用

## 项目结构

- `main.go` — 入口、ARP 扫描逻辑
- `server.go` — HTTP 服务器、API 路由
- `daemon.go` — 后台守护、数据持久化、月度报告
- `index.html` — 前端页面
