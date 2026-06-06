# 📡 摸鱼雷达

基于 ARP 的局域网设备在线监测工具，定时扫描网络设备并记录在线时长，提供 Web 可视化面板。

## 功能特性

- **ARP 扫描** — 局域网设备发现，支持指数退避重试（最多 3 次）
- **定时轮询** — 每 5 分钟扫描一次，记录 06:00 ~ 24:00 时段数据
- **数据持久化** — SQLite 存储，自动清理半年前旧数据
- **Web 面板** — 今日设备详情、月度排行报告
- **别名管理** — MAC 地址映射姓名，支持 Web 端注册/删除
- **月度报告** — 每月 1 号自动生成 Markdown 格式报告
- **单次扫描** — `-once` 模式，不写入数据库，适合快速排查

## 快速开始

### 环境要求

- Go 1.21+
- Linux（需要 root 权限发送 ARP 包）
- 同一局域网内

### 编译

```bash
make build
```

### 运行

```bash
make run
```

默认监听 `http://<设备IP>:9527`

### 单次扫描

```bash
make once
```

输出扫描到的设备 IP、MAC 地址和响应时间，不写入数据库。

## Docker 部署

### 构建镜像

```bash
make docker-build
```

### 推送到私有仓库

```bash
make docker-push
```

### docker-compose

```bash
docker-compose up -d
```

容器使用 `host` 网络模式 + `privileged` 权限，数据持久化到 `./data/moyu.db`。

## 命令行参数

| 参数 | 说明 |
|------|------|
| `-once` | 单次扫描模式，不启动 HTTP 服务和守护进程 |

## 配置说明

### 扫描参数

定义在 `main.go` 顶部常量，按需修改后重新编译：

| 常量 | 默认值 | 说明 |
|------|--------|------|
| `maxRetries` | 3 | 每个目标最大重试次数 |
| `initialTimeout` | 1s | 首次超时 |
| `backoff` | 1.5 | 退避倍数 |
| `globalTimeout` | 10s | 单次扫描全局超时 |
| `packetInterval` | 4ms | ARP 包发送间隔 |

### 其他配置

- **记录时段**: 06:00 ~ 24:00（`daemon.go` 中 `inWindow` 判断）
- **扫描间隔**: 5 分钟（固定）
- **数据保留**: 自动清理 6 个月前数据（每天执行一次）
- **HTTP 端口**: 9527（固定）

## Web 面板

访问 `http://<设备IP>:9527`，功能包括：

- **今日详情** — 显示当日所有设备在线时长，绿色圆点表示当前在线
- **月度报告** — 按月查看已注册设备的在线天数和总时长排行
- **别名管理** — 点击「管理」按钮或 MAC 地址，注册/删除用户别名

## API 接口

### GET /api/status

返回当前扫描状态。

```json
{
  "slots": 120,
  "online": ["aa:bb:cc:dd:ee:ff", "11:22:33:44:55:66"]
}
```

- `slots`: 当天已过的 5 分钟轮数（06:00 起算）
- `online`: 当前在线设备 MAC 列表

### GET /api/daily?date=2026-06-06

查询指定日期的设备在线数据。

```json
{
  "date": "2026-06-06",
  "slots": 120,
  "devices": [
    {"mac": "aa:bb:cc:dd:ee:ff", "name": "张三", "cnt": 50, "hours": 4.17}
  ]
}
```

- `date`: 查询日期，默认当天
- `cnt`: 该设备被检测到在线的次数（每 5 分钟 +1）
- `hours`: 换算后的在线小时数

### GET /api/monthly?ym=2026-06

查询指定月份的设备汇总数据。

```json
{
  "ym": "2026-06",
  "days": 6,
  "devices": [
    {"mac": "aa:bb:cc:dd:ee:ff", "name": "张三", "days": 5, "hours": 40.5}
  ]
}
```

- `ym`: 查询月份（YYYY-MM），默认当月
- `days`: 该月总天数

### GET /api/aliases

获取所有已注册别名。

```json
[
  {"mac": "aa:bb:cc:dd:ee:ff", "name": "张三"}
]
```

### POST /api/aliases

注册或更新别名。

```json
{"mac": "aa:bb:cc:dd:ee:ff", "name": "张三"}
```

### DELETE /api/aliases?mac=aa:bb:cc:dd:ee:ff

删除别名。

## 项目结构

```
├── main.go            入口、网络接口发现、ARP 扫描逻辑
├── daemon.go          后台守护进程、数据持久化、旧数据清理、月度报告生成
├── server.go          HTTP 服务器、API 路由、前端静态资源（embed）
├── index.html         前端单页应用（纯 HTML/CSS/JS）
├── Dockerfile         Docker 镜像构建（distroless 基础镜像）
├── docker-compose.yaml
├── Makefile           构建/运行/清理/Docker 命令
├── go.mod / go.sum    Go 依赖管理
├── moyu.db            SQLite 数据库（运行时生成）
└── README.md
```

## 技术栈

| 层级 | 技术 |
|------|------|
| 语言 | Go |
| 网络扫描 | [mdlayher/arp](https://github.com/mdlayher/arp) |
| 数据库 | SQLite（[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)，纯 Go 实现，无 CGO） |
| HTTP 服务 | Go 标准库 `net/http` |
| 前端 | 纯 HTML/CSS/JS，无框架依赖 |
| 嵌入 | Go `//go:embed` 指令 |
| 容器 | Docker + distroless 基础镜像 |

## 数据库表结构

```sql
-- 每日设备在线次数
CREATE TABLE daily (
    date TEXT NOT NULL,    -- 日期 YYYY-MM-DD
    mac  TEXT NOT NULL,    -- MAC 地址
    cnt  INTEGER DEFAULT 0, -- 被检测到在线的次数
    PRIMARY KEY (date, mac)
);

-- 设备别名
CREATE TABLE aliases (
    mac  TEXT NOT NULL PRIMARY KEY, -- MAC 地址
    name TEXT NOT NULL              -- 姓名
);
```
