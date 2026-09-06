# DeepSeek Harness 控制台

一个自包含的 **DeepSeek Harness (dsh) 管理控制台**。它在 fnOS（TRIM 平台）上负责
拉起并守护 `dsh web` 进程，并通过一个内嵌的 Vue 管理界面提供：
概览、设置、目录授权、插件管理、Web 终端、日志查看与更新管理等功能。

本仓库（`shuangji66/dsh`）即 DeepSeek Harness 控制台的后端 + 前端源码。

> 它**不是** `dsh` 本体，而是管理 `dsh` 的“控制台”。dsh 服务本身由
> `.github/workflows/server-build.yaml` 从 `@deepseek-ai/dsh` npm 包构建发布。

---

## 功能特性

- **守护 dsh 进程** — 启动、停止、重启、状态（CPU / 内存 / PID / 端口），并自动在
  dsh 装插件自重启后通过 `/proc` 重新发现实时 PID。
- **访问 Token / Cookie 交换** — 从 dsh 启动日志捕获一次性访问 token，换取 dsh 会话
  Cookie，供反向代理转发时携带，实现免 token 访问。
- **反向代理** — 把 dsh 的 Web 界面经统一端口（默认 `13079`）对外暴露，并叠加登录鉴权。
- **登录鉴权** — 密码校验（≥8 位、大小写字母/数字/符号组合）、会话 Cookie、TTL
  有效期，以及访客管理（在线访客列表 / 踢出，SSE 实时推送）。
- **Web 终端** — 基于 `creack/pty` + xterm.js 的交互式 bash 会话，通过 WebSocket 传输。
- **目录授权** — 授权/查看已共享目录，并可将某已授权目录设为 dsh 主目录。
- **插件管理** — 列出 / 移除 / 重置 dsh web profile 的插件依赖。
- **快捷指令** — 持久化的终端快捷命令（`HARNESS_QUICK_CMDS_FILE`）。
- **日志** — 查看 / 下载 / SSE 实时流式输出 dsh 与主进程日志（`HARNESS_LOG_FILE`）。
- **更新管理** — 自动检测 harness 控制台 / dsh 服务的新版本（每小时），支持多 GitHub
  加速源回退，并可一键应用更新、回滚（数据备份 / 恢复）。
- **node-pty 自动安装** — 主进程启动后自动补齐 `node-pty` 预构建文件与 patch。

---

## 技术栈

| 层 | 技术 |
| --- | --- |
| 后端 | Go（`backend/`），标准库 + `github.com/creack/pty` |
| 前端 | Vue 3（Composition API / `<script setup>`）+ TypeScript + Vite |
| 前端构建 | Tailwind CSS、Pinia、Vue Router、xterm.js（`@xterm/xterm`） |
| 通信 | Admin Unix Socket、HTTP JSON API、WebSocket（终端）、SSE（日志/访客） |
| 部署目标 | fnOS / TRIM 平台，`/var/apps/Harness`，经 nginx 前置 |

---

## 目录结构

```
.
├── Makefile                 # make dev / release / clean（前端→embed→Go 单二进制）
├── build.sh                 # 等价构建脚本
├── backend/                 # Go 后端
│   ├── main.go              # 入口：runtime env、日志、启动 dsh、反向代理、优雅退出
│   ├── config.go            # 应用配置 & 运行时环境（环境变量解析）
│   ├── admin.go             # Admin 管理 mux：SPA、API 路由、Unix socket 服务
│   ├── auth.go              # 登录鉴权（Cookie / HMAC / 密码校验）
│   ├── dsh.go               # DshManager：dsh 进程生命周期 / token 交换 / 状态 / 插件
│   ├── proxy.go             # 反向代理（携带 dsh 会话 Cookie）
│   ├── terminal.go          # WebSocket 交互式 PTY 终端
│   ├── update.go            # 更新管理（harness / dsh 服务，加速源，回滚/备份）
│   ├── install.go           # node-pty 自动安装
│   ├── quickcmds.go         # 终端快捷指令持久化 API
│   ├── visitors.go          # 访客跟踪 / 踢出 / SSE
│   ├── sse.go               # Server-Sent Events 推送
│   └── fnos.go              # fnOS open-gateway 客户端（路径转换等）
└── frontend/                # Vue 3 管理界面
    ├── index.html           # 注入 <base> 由后端运行时改写
    ├── vite.config.ts       # 相对 base + 运行时 baseurl 方案
    └── src/
        ├── main.ts / App.vue / style.css
        ├── router/index.ts  # /?view= 切换 + 旧路径 302 重定向
        ├── stores/          # Pinia：settings / toast / plugins / directories
        ├── composables/     # useTheme / useI18n / useConsolePrefs
        ├── serverapi/index.ts   # 运行时 baseurl 感知的 API / SSE 客户端
        ├── components/      # Toast / ConfirmDialog / 快捷指令 / 更新区
        └── views/           # 概览 / 设置 / 目录 / 插件 / 终端 / 日志
```

---

## 构建

> 前置：Go ≥ 1.26、Node.js ≥ 24。

构建会把前端 `dist` 拷入 `backend/embed`，再用 `//go:embed` 把静态资源打进行内嵌进
Go 二进制，最终生成**单文件自包含**的 `backend/harness`。

```bash
# 开发构建（默认，非 stripped）
make            # 或 ./build.sh

# Release 构建（strip + 外部链接），可指定版本号
make release V=1.0.1

# 清理构建产物
make clean
```

两种方式等价；`build.sh` 额外内置了 `nodejs` 的 PATH 与版本号参数：

```bash
./build.sh               # dev
./build.sh release 1.0.1 # release + 指定版本
```

GitHub Actions（`.github/workflows/`）提供 CI 构建：

- `build.yml` — 手动触发，交叉编译 `amd64` / `arm64` 的 harness 二进制。
- `harness-build.yaml` — 手动触发，构建 harness 并发布到 GitHub Release。
- `server-build.yaml` — 每 8 小时自动从 `@deepseek-ai/dsh` 打包 server（含
  `dshmarket` 依赖并注入 `PROFILE_TEMPLATES.web.bundles`），发布 Release。

---

## 运行 / 环境变量

二进制经 fnOS 平台以 `/var/apps/Harness` 部署，监听 **Admin Unix Socket**
（默认 `<appDest>/app.sock`），前端由 nginx 反代到该 socket 的 baseurl 前缀。

| 环境变量 | 说明 | 默认 |
| --- | --- | --- |
| `HARNESS_CONFIG_FILE` | 配置文件路径 | `$TRIM_PKGVAR/config.json` |
| `HARNESS_ADMIN_SOCK` | Admin Unix socket 路径 | `$TRIM_APPDEST/app.sock` |
| `HARNESS_ADMIN_BASEURL` | 前端资源 baseurl 前缀 | `$TRIM_APPDEST` |
| `HARNESS_LOG_FILE` | 日志落盘路径（空则不落盘） | 空 |
| `HARNESS_PID_FILE` | PID 文件路径 | 空 |
| `HARNESS_AUTOSTART` | 设为 `0` 时不自动启动 dsh | `1` |
| `HARNESS_QUICK_CMDS_FILE` | 终端快捷指令持久化文件 | `$TRIM_PKGVAR/quickcmds.json` |
| `PROXY_PORT` | 反向代理监听端口 | `13079` |
| `dsh_port` / `TARGET_PORT` | dsh web 端口 | `13080` |
| `proxy_mode` | 设为 `1` 启用代理 | `0` |
| `proxy_addr` | 代理地址 | `http://127.0.0.1:7890` |
| `auth_mode` / `PROXY_AUTH` | 启用鉴权 | `true` |
| `password` | 登录密码 | 空 |
| `auth_ttl_hours` | 登录鉴权有效期（小时） | `2` |
| `TRIM_API_TOKEN` / `TRIM_APPNAME` | fnOS gateway 凭据 | — |

运行时配置（`config.json`）字段：`dshPort`、`proxyEnabled`、`proxyAddr`、
`authEnabled`、`password`、`authTTLHours`、`dshMemLimit`、`dshMemAuto`、
`homeDir`、`accessUrls`。可通过设置页修改并保存。

---

## 主要流程

1. **启动** — 解析环境变量 → 读取配置 → 校验密码 → 启动 Admin socket → 自动启动
   `dsh web --no-open --port <port>`（除非 `HARNESS_AUTOSTART=0`）。
2. **凭据交换** — 从 dsh 日志扫描一次性访问 token（`?token=`），用它访问一次
   dsh 地址，从 `Set-Cookie` 换取 `dsh-auth-*` 会话 Cookie。
3. **反向代理** — 携带该 Cookie 把 dsh 反代到 `PROXY_PORT`，叠加登录鉴权。
4. **node-pty** — 等待 `$HOME/.dsh/profiles/web` 目录生成后安装并 patch node-pty。
5. **退出** — 收到 `SIGINT/SIGTERM/SIGQUIT` 或 `stopCh` 后停止 dsh、移除 socket。

---

## 常见问题排查

- **Admin socket 已被占用** — 说明已有实例在运行，主进程会直接退出。
- **鉴权未启用** — 未设置 `password` 或密码强度校验失败时后端会打印警告，任何人可访问。
- **反代不带凭据** — 旧版 dsh 或日志未就绪导致未捕获 token 时，反代将不带 Cookie。
- **CPU/内存读不到** — dsh 装插件自重启后 PID 变化，后端会自动在 `/proc` 中重新发现。

---

## 许可

[MIT](LICENSE) © 2026 shuangji66

更多设计背景与说明见 [`genesis-DESIGN.md`](genesis-DESIGN.md)。