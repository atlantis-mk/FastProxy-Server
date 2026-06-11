# FastProxy Server

FastProxy 的 Go 后端骨架，目标是支撑两类核心能力：

- 动态切换代理内核，例如 `mihomo` 和 `sing-box`
- 管理多份 JSON 代理配置，并在运行时切换当前生效配置

## 当前能力

- 基础 HTTP API 服务
- 本地数据目录初始化
- 内核清单与当前选中内核管理
- 内核二进制来源管理：配置路径、`cores/<core>/<version>` 本地缓存、上传安装和 GitHub Release 显式更新
- 代理配置的增删改查
- 配置内容使用 JSON 持久化并在写入时校验
- 当前激活配置管理
- mihomo 与 sing-box 运行时进程管理、重启、编译后启动、日志采集和 pending apply 状态

## 目录结构

```text
cmd/fastproxy-server      程序入口
internal/api             HTTP API
internal/appconfig       启动配置
internal/appdata         数据目录管理
internal/core            内核与运行时管理框架
internal/httpjson        JSON 响应工具
internal/profile         代理配置存储
```

## 数据目录

FastProxy 后端数据目录由 `FASTPROXY_SERVER_DATA_DIR` 指定。仓库配置和可查询缓存采用双层存储：

- `repository/subscriptions/*.json`：订阅 source 配置，保留为用户可读 JSON。
- `repository/node-sets/*.json` 与 `repository/node-sets/_managed.json`：手写节点集和托管节点集 source 配置。
- `repository/routing-rule-sets/*.json`、`repository/sing-box-rule-sets/*.json`、`repository/mihomo-rule-providers/*.json`、`repository/group-sets/*.json`、`repository/profiles/*.json`：小型用户配置，继续以 JSON 保存。
- `repository/rule-source-repositories/*.json`：规则仓库 source 配置。
- `repository/rule-source-indexes/rule-source-indexes.sqlite`：本地可查询数据，包含规则仓库索引、节点缓存、健康检查历史、操作事件日志和运行时快照摘要。
- `runtime/active.json` 与 `runtime/last-good.json`：完整生成后的运行时配置文件。SQLite 只保存路径、hash、摘要和诊断，不保存完整配置正文。
- `cores/<core>/<version>/`：核心二进制缓存。上传文件和 GitHub Release 下载都会规范化到这套目录，执行时优先使用显式配置路径，其次使用最新有效缓存。
- `settings/github.json`：本地 GitHub token 设置，仅后端读取明文；API 只返回是否已配置和更新时间。

SQLite 中的数据用于分页、搜索、历史和诊断，可通过刷新订阅、刷新仓库索引或重新编译运行时从 JSON source 配置恢复。

## 启动

```bash
go run ./cmd/fastproxy-server
```

默认监听地址：

```text
127.0.0.1:43171
```

可选环境变量：

```text
FASTPROXY_SERVER_ADDR
FASTPROXY_SERVER_DATA_DIR
FASTPROXY_SERVER_LOG_LEVEL
FASTPROXY_SERVER_MIHOMO_BIN
FASTPROXY_SERVER_SING_BOX_BIN
FASTPROXY_SERVER_GITHUB_TOKEN
GITHUB_TOKEN
```

`FASTPROXY_SERVER_MIHOMO_BIN` 和 `FASTPROXY_SERVER_SING_BOX_BIN` 始终是最高优先级二进制来源。未设置时，后端会扫描本地 `cores/` 缓存；GitHub 下载只会在显式检查或安装更新时发生。

## macOS 安装为系统服务

打包后的后端二进制可以命名为 `fastproxy`，并用 root 权限安装为 LaunchDaemon：

```bash
sudo fastproxy install
```

安装命令会复制当前二进制到：

```text
/Library/PrivilegedHelperTools/fastproxy-server
```

并写入、加载和启动：

```text
/Library/LaunchDaemons/com.fastproxy.server.plist
```

默认服务数据目录为 `/Library/Application Support/FastProxy`，日志目录为 `/Library/Logs/FastProxy`，监听地址仍为 `127.0.0.1:43171`。可以在安装时覆盖：

```bash
sudo fastproxy install \
  --addr 127.0.0.1:43171 \
  --data-dir "/Library/Application Support/FastProxy" \
  --mihomo-bin /path/to/mihomo \
  --sing-box-bin /path/to/sing-box
```

如只安装文件、不立即启动：

```bash
sudo fastproxy install --no-start
```

## API

- `GET /api/health`
- `GET /api/bootstrap`
- `GET /api/cores`
- `PUT /api/cores/{core}/path`
- `POST /api/cores/{core}/upload`
- `POST /api/cores/{core}/updates/check`
- `POST /api/cores/{core}/updates/install`
- `GET /api/settings/github-token`
- `PUT /api/settings/github-token`
- `DELETE /api/settings/github-token`
- `GET /api/runtime/status`
- `PUT /api/runtime/core`
- `POST /api/runtime/start`
- `POST /api/runtime/stop`
- `POST /api/runtime/restart`
- `POST /api/runtime/compile-and-start`
- `POST /api/runtime/restart-and-apply`
- `GET /api/profiles`
- `POST /api/profiles`
- `GET /api/profiles/{id}`
- `PUT /api/profiles/{id}`
- `DELETE /api/profiles/{id}`
- `GET /api/profile-state`
- `PUT /api/profile-state/active`

## 运行时行为

启动核心时，后端使用当前激活的 `runtime/active.json` 快照，而不是直接读取正在编辑的资源。`sing-box` 会物化为 `runtime/sing-box-active.json` 并执行 `sing-box check -c` 后运行；`mihomo` 会物化为 `runtime/mihomo-active.yaml` 并执行 `mihomo -t -f` 后运行。

如果运行中切换 selected core，后端会保留当前进程并报告 pending apply。使用 `POST /api/runtime/restart-and-apply` 可重新编译当前激活 profile，并用最新 selected core 重启。
