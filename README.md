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
- **浏览器兼容模式** — 可开关的反代注入，修复 Firefox / Zen / Safari 等非 V8 引擎上
  「会话历史无法加载」的问题；默认关闭，Chromium 开启无副作用。
  （见上文「浏览器兼容模式」一节）
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
| `HARNESS_PID_FILE` | PID 文件路径（harness 控制台自身 PID，恒不变） | 空 |
| `HARNESS_DSH_PID_FILE` | dsh 服务 PID 文件路径（随 dsh 启动/自重启刷新为实时 PID，dsh 停止时移除） | 空 |
| `HARNESS_AUTOSTART` | 设为 `0` 时不自动启动 dsh | `1` |
| `HARNESS_QUICK_CMDS_FILE` | 终端快捷指令持久化文件 | `$TRIM_PKGVAR/quickcmds.json` |
| `PROXY_PORT` | 反向代理监听端口 | `13079` |
| `dsh_port` / `TARGET_PORT` | dsh web 端口 | `13080` |
| `proxy_mode` | 设为 `1` 启用代理 | `0` |
| `proxy_addr` | 代理地址 | `http://127.0.0.1:7890` |
| `auth_mode` / `PROXY_AUTH` | 启用鉴权 | `true` |
| `password` | 登录密码 | 空 |
| `auth_ttl_hours` | 登录鉴权有效期（小时） | `4` |
| `TRIM_API_TOKEN` / `TRIM_APPNAME` | fnOS gateway 凭据 | — |

运行时配置（`config.json`）字段：`dshPort`、`proxyEnabled`、`proxyAddr`、
`authEnabled`、`password`、`authTTLHours`、`dshMemLimit`、`dshMemAuto`、
`homeDir`、`accessUrls`、`browserCompat`。可通过设置页修改并保存。

---

## 浏览器兼容模式（`browserCompat`）

**用途** — 修复 **Firefox / Zen（SpiderMonkey）** 与 **Safari / 苹果设备（JavaScriptCore）**
上「会话历史一直显示『载入历史…』、且 AI 输出后无法恢复实时对话」的问题。默认**关闭**，
在设置页「node 栈内存限制」与「启用登录鉴权」之间切换；Chromium 内核（Chrome / Edge）
开启无副作用。

### 问题成因

dsh 客户端 bundle 中有一处只适配 V8 的原生函数格式判断：

```js
Function.prototype.toString.call(constructor) === `function ${name}() { [native code] }`
```

非 V8 引擎把原生函数源码格式化为**多行**（实测 Firefox 156 与 WebKit 均为
`"function Object() {\n    [native code]\n}"`），该比较恒为 `false`，于是普通对象被判为
「非本 realm 原生原型」→ `snapshotJsonValue` 返回 `undefined` → 客户端抛
`TypeError: Assistant stream raw chunk must be a lossless JSON object`，历史渲染在首条消息
前中止。

由于该错误是普通 `TypeError` 而非 `RemoteError`，dsh 不会把它归类为可重试的远端失败，
`openState` 永久停留在 `"loading"`，因此**必须刷新页面或切换会话**才能恢复。

触发条件是「**流式进行中刷新**」：该校验（`expandAssistantStream`）只在处理
`assistantStream.activeAttempt.stream`（正在进行的尝试的 baseline）时执行，已完成的
历史记录不走这条路径。所以在 Chromium 上难以复现，长会话即便含大量 chunk 记录也正常。

### 修复方式

在反代转发路径上把该比较替换为**运行时受开关控制**的等价表达式（比较前把空白折叠为
单个空格，使各引擎统一到 V8 形态）。该变换对 V8 是恒等变换，V8 下原本通过的判断依旧
通过，不会放宽任何实际约束（该判断是「重复安装 / 跨 realm」启发式，非安全边界）。

### 生效时机与缓存（重要）

dsh 的插件资源（`/plugins/??…&rev=<dsh 自己的 rev>`）响应带：

```
Cache-Control: public, max-age=31536000, immutable
```

且**无 `ETag` / `Last-Modified`**；`rev` 由 dsh 自身生成，**不随 harness 升级而改变**，
且该路由严格校验 `rev`（改写或省略一律 404），因此 URL 无法被 harness 改写以击穿缓存。
实测结论：

| 场景 | 资源字节 | 普通刷新（F5） |
| --- | --- | --- |
| **切换开关**（开 ↔ 关） | 不变 | ✅ 生效 |
| **升级 harness 后**（首次引入 / 变更该修复） | 变化 | ❌ 不生效，需清缓存 |

开关状态不写进 bundle 字节，而是由**每次都回源的 HTML** 注入
`window.__DSH_BROWSER_COMPAT__`，所以「切开关 → 普通刷新」即可生效；但若浏览器本地缓存
仍是**升级前**的旧 bundle 字节，普通刷新不会回源，**必须清除浏览器缓存或强制刷新
（Ctrl+Shift+R）**才能拿到新字节。设置页的开关提示中已说明这一点。

### 与「会话自愈探针」的区别

反代另有一处注入（`sessionWatchdogInit`，见 `backend/proxy.go`）**与本开关无关、始终启用**：
它修复的是 dsh `ClientSessions.followCurrent()` 仅在 `current !== watched` 时打开事件窗口、
一旦 `open()` 失败便无任何重试路径的缺陷（`connection/reset` 只刷新列表不重建窗口，
`resync()` 在 `cold` 状态下又是空操作）。该缺陷与浏览器内核无关，Chromium 同样会遇到。

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
- **Firefox / Safari 打开会话只有「载入历史…」（且一直显示「深度求索中…」）** —
  非 V8 引擎的已知问题，开启设置页的「浏览器兼容模式」。若开启后仍无效，是浏览器
  仍在使用升级前的旧 bundle 缓存：**清除浏览器缓存或强制刷新（Ctrl+Shift+R）**；
  详见上文「浏览器兼容模式」一节。
- **改动了反代注入 / 前端后刷新看不到变化** — dsh 插件资源带一年期 `immutable` 强缓存
  且无 `ETag`，普通刷新不回源。清除浏览器缓存或强制刷新；必要时重启 dsh 使其 `rev` 变化。

---

## 许可

[MIT](LICENSE) © 2026 shuangji66

更多设计背景与说明见 [`genesis-DESIGN.md`](genesis-DESIGN.md)。