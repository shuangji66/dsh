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
- **反向代理** — 把 dsh 的 Web 界面经配置的端口（默认 `3079`，设置页可改）对外暴露，并叠加登录鉴权。
  **控制台启动的第一刻就监听**：dsh 还没起来时不再“无响应”，而是先给登录页、再给
  带阶段的等待页，dsh 完成启动（含换取凭据、安装依赖）后等待页自动跳转。
  （见下文「启动期间的等待页与放行门禁」一节）
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
- **更新管理** — 自动检测 harness 控制台 / dsh 服务 / 插件市场的新版本（每小时），
  下载走「代理 / 直连」两条通路（各 2 次机会，支持暂停与断点续传），
  并可一键应用更新、回滚（数据备份 / 恢复）。
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
│   ├── main.go              # 入口：runtime env、日志、启动反代、启动 dsh、优雅退出
│   ├── boot.go              # 启动阶段状态机（等待页文案 / 反代放行门禁的输入）
│   ├── config.go            # 应用配置 & 运行时环境（环境变量解析）
│   ├── admin.go             # Admin 管理 mux：SPA、API 路由、Unix socket 服务
│   ├── auth.go              # 登录鉴权（Cookie / HMAC / 密码校验）
│   ├── dsh.go               # DshManager：dsh 进程生命周期 / token 交换 / 状态 / 插件
│   ├── proxy.go             # 反向代理（携带 dsh 会话 Cookie）+ 等待页 / /_ready
│   ├── terminal.go          # WebSocket 交互式 PTY 终端
│   ├── update.go            # 更新管理（版本检测、下载通路/续传、回滚/备份）
│   ├── market.go            # 插件市场（dshmarket）就地更新
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
反向代理本身有**两条监听**：TCP 端口（配置项 `proxyPort`，默认 `3079`，占据站点根）
与可选的 **Unix Socket 子路径挂载**（`HARNESS_PROXY_SOCK` +
`HARNESS_PROXY_BASEURL`，见下文「子路径部署」）。

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
| `HARNESS_PROXY_SOCK` | 反向代理的子路径挂载 Unix socket（空或 `off` 关闭） | `$TRIM_APPDEST/dsh.sock` |
| `HARNESS_PROXY_BASEURL` | 该 socket 对外占据的子路径（反代剥掉后再转发给 dsh） | `/app/Harness/dsh` |
| `PROXY_PORT` | **已废弃**：反代监听端口改为设置页配置项（`config.json` 的 `proxyPort`，默认 `3079`），此环境变量不再生效 | — |
| `dsh_port` / `TARGET_PORT` | dsh web 端口 | `13080` |
| `proxy_mode` | 设为 `1` 启用代理 | `0` |
| `proxy_addr` | 代理地址 | `http://127.0.0.1:7890` |
| `auth_mode` / `PROXY_AUTH` | 启用鉴权 | `true` |
| `password` | 登录密码 | 空 |
| `auth_ttl_hours` | 登录鉴权有效期（小时） | `4` |
| `TRIM_API_TOKEN` / `TRIM_APPNAME` | fnOS gateway 凭据 | — |

运行时配置（`config.json`）字段：`dshPort`、`proxyPort`、`proxyEnabled`、`proxyAddr`、
`authEnabled`、`password`、`authTTLHours`、`dshMemLimit`、`dshMemAuto`、
`homeDir`、`accessUrls`、`browserCompat`。可通过设置页修改并保存。

其中 `proxyPort`（反代本身对外监听的 TCP 端口，设置页「反代监听端口」）与 `dshPort`
语义不同：`dshPort` 由 dsh 进程绑定、必须停 dsh 才能改；`proxyPort` 是 harness 自己的
监听，保存时先绑定新端口、成功后才关闭旧监听并落盘，因此改完立即生效，端口被占用则
整次保存被拒绝（旧监听继续服务）。

---

## 子路径部署：反代挂载点与 Unix Socket 前置

除了 TCP 端口（根挂载，端口见配置项 `proxyPort`），反代还会在
**`HARNESS_PROXY_SOCK`（默认 `$TRIM_APPDEST/dsh.sock`）** 上再监听一个 Unix
Socket，并把它挂在 **`HARNESS_PROXY_BASEURL`（默认 `/app/Harness/dsh`）** 子路径下：

```text
浏览器 https://<fnip>:<port>/app/Harness/dsh/…
      ↓ 平台网关（fnOS open-gateway）原样转发（路径带前缀）
Harness 反代 @ unix socket
      ↓ 剥掉 /app/Harness/dsh，补上 dsh 会话 Cookie
dsh web @ 127.0.0.1:13080   ← 只认 /、/api、/plugins
```

默认值刻意比控制台 baseurl（`/app/Harness`，走 admin socket）**深一层**，这样同
一台设备上两条线各占一段路径、互不抢：控制台在 `/app/Harness/…`，dsh GUI 在
`/app/Harness/dsh/…`。

**为什么这样就成立**：dsh 0.1.7-alpha.1 起其前端产物全部使用**文档相对路径**
（`dsh-host-frontend-static` 注入 `<base href="./">`，插件 bundle 引用、`/api`
RPC、HMR 的 SSE、流 mux 的 WebSocket 都是去前导斜杠的相对形式）。页面在
`/app/Harness/dsh/` 下加载时，浏览器自动把请求拼成 `/app/Harness/dsh/api/...`；
反代剥掉前缀后 dsh 收到的仍是它认识的 `/api/...`。**dsh 侧不需要任何 baseurl
配置**，配对成立的唯一条件是反代把前缀剥干净。

反代的实现要点（`proxyMount`，见 `proxy.go`）：

- 进站路径先剥前缀，后续鉴权、握手、就绪门禁、等待页、转发全部工作在挂载内路径上；
- 反代自留路径与跳转目标补回前缀：`/_login`、`/_logout`、`/_ready`、未登录时的
  302、登录成功后的 `next`、等待页的轮询地址与 `Refresh` 兜底 URL；
- 裸挂载点（`/app/Harness/dsh` 无尾斜杠）301 到 `/app/Harness/dsh/`：dsh 前端的
  `<base href="./">` 以**目录**为基准，缺尾斜杠会让相对路径解析到站点根；
- 不属于该挂载的路径直接 404（网关配置错误时不会把流量悄悄转给 dsh）。

部署时注意：

- **dsh 需 ≥ 0.1.7-alpha.1**。更早版本注入的是 `<base href="/">` 且 API/SSE/WS
  用绝对根路径，在子路径下必然 404（正是该版本的修复项）。
- 网关若**不**剥离前缀（原样把 `/app/Harness/dsh/...` 交给本 socket），反代也能
  正确剥离——前缀本来就由反代处理，网关只需保证路径原样透传、不额外改写。
- **网关/nginx 必须按更长的前缀优先匹配**：控制台那条规则（admin socket，
  `/app/Harness`）在前缀上包含 dsh 这条（`/app/Harness/dsh`），先匹配到控制台就
  会把 dsh 的流量截走。nginx 天然按最长前缀匹配，自建规则时注意这一点。
- 该监听是可选的：目录不可创建 / socket 被占用 / 监听失败时只记日志并跳过，
  不影响控制台与 TCP 反代启动。`HARNESS_PROXY_SOCK=off` 可显式关闭。

### 概览页的「飞牛入口」

概览页「快捷访问」的第一行固定显示**飞牛入口**——当前访问环境下经平台网关访问
dsh 服务的地址，无需用户配置：控制台的当前访问地址（由请求的 `Origin` / `Referer`
推断，缺失时退回 `Host` + `X-Forwarded-Proto`）**剥离控制台 baseurl**
（`HARNESS_ADMIN_BASEURL`）得到飞牛 OS 的访问源，再拼接 dsh 服务挂载的 baseurl
（`HARNESS_PROXY_BASEURL`）：

```text
控制台 http://192.168.1.111:5666/app/Harness
  ├─ 剥离 /app/Harness      → 飞牛 OS 访问源 http://192.168.1.111:5666
  └─ 拼接 /app/Harness/dsh  → http://192.168.1.111:5666/app/Harness/dsh
```

换算在 `backend/admin.go` 的 `fnosEntryURL` 完成，随 `GET /api/settings` 的
`runtime.fnosEntryURL` 下发；后端拿不到访问地址时前端退回浏览器自身地址
（`document.baseURI` 同样剥掉 baseurl）自行换算。未启用网关子路径挂载
（`HARNESS_PROXY_BASEURL` 为空或 `/`）时该行不显示，避免给出指向站点根的错地址。

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

### 与「会话自愈探针」的关系（已移除）

反代曾额外注入一处与本开关无关、始终启用的「会话自愈探针」（旧 `sessionWatchdogInit`）：
它针对的是 dsh `ClientSessions.followCurrent()` 仅在 `current !== watched` 时打开事件窗口、
一旦 `open()` 失败便无重试路径的缺陷。

新版 dsh 已重构该机制：`sessions.list` 快照不再有 `current` 字段，`followCurrent()` 方法
已被删除，改由视图层显式 `sessions.retain(target, { source: "mainView" })` 打开窗口。
探针的入口条件（`snapshot.current`）因此恒不成立，只剩一个空转定时器，故**已移除**。
详见 `backend/proxy.go` 的 `rewriteJSBundle` 注释。

---

## 插件市场（dshmarket）的更新

**背景**：本项目的 dshmarket 不是 profile 依赖，而是构建 server 包时被写进
`@deepseek-ai/dsh` 的 `dependencies`（见 `.github/workflows/server-build.yaml` 与
`.tools/harness-fpk-build.yaml`）。安装后它落在
`<server>/node_modules/@deepseek-ai/dsh/node_modules/dshmarket`，dsh 启动时再把它镜像成
`$DSH_HOME/profiles/node_modules/dshmarket` —— **市场跑的是哪一份字节，由 server 目录决定**。

由此有两个后果：

- 市场面板里的 `selfManaged` 恒为 `false`（它不在 profile `package.json` 的 `dependencies`
  里），面板**没有**自更新入口，直接打市场的 `/dsh-market/update` 也会被
  `plugin is not installed` 挡掉；
- dsh server 包只在 `@deepseek-ai/dsh` 有新版本时才会重建，于是 dsh 版本空窗期内，
  市场会一直停在构建 server 包时 npm 解析出来的那个版本。

**所以控制台自己提供入口**：概览页的「插件市场版本」一行（`backend/market.go`），
与 harness / dsh 一样分「下载 → 安装」两步，走同一套 SSE 进度与二次确认：

- 版本来自 npm registry（`npmmirror` → 腾讯云镜像 → `registry.npmjs.org` 依次回退），
  下载后校验 `dist.integrity`（SRI，缺失时退 `dist.shasum`）；**两个都没有则拒绝安装**。
- 安装阶段顺序：校验新包（包名/版本/`lib/`/`dsh.bundle.patch`/`exports["./client"]`，
  以及非 `@deepseek-ai/*` 依赖在目标位置可解析）→ **停止 dsh** → 备份当前目录到
  `TRIM_PKGVAR/backup/market-<旧版本>-<时间戳>.tar.gz` → staging + `rename` 原子替换 →
  **自动拉起 dsh 并重新换取会话 token**（`startDshCaptured`）。
- **停 dsh 之前先确认没有插件操作在跑**：插件的安装/卸载都在 dsh 进程内持有 profile
  写锁，而停 dsh 会把它连根拔掉并留下陈旧锁（安装本身也白做）。
  - 替换 `server/` 产物的三条路径（**更新 dsh 服务 / 更新市场 / 回滚 server 备份**）
    统一走 `stopDshForReplacement()`：先过 `replaceBusyGuard()`，忙则拒绝并说明原因
    （此时**不产生任何停机、也不改盘**），空闲才停 dsh 并等端口释放。
  - **更新 harness 控制台**与**恢复 dsh 数据**（后者会删掉整个 `~/.dsh`）也在动手前
    先过同一道 `replaceBusyGuard()`。
  - 守卫的两个来源互补：市场面板发起的安装 → 查 `/dsh-market/status` 的 `busy`；
    控制台插件页发起的命令 → 查 `DshManager.PluginCmdRunning()`。
  - 市场不回答（老版本没这个路由 / dsh 已经坏了）时视为「不忙」，所以回滚、恢复这类
    抢修操作不会被守卫挡住。
  - **例外（刻意不挡，但会提示）**：概览页的「停止 / 重启 dsh 服务」按钮 —— 那是用户明确的
    即时意图。弹窗打开时会查一次 `GET /api/dsh/busy`（市场 `/dsh-market/status` 的 busy +
    控制台插件命令计数），有插件操作在跑就多显示一条**风险提示**（说明会中断它、可能留下陈旧
    写锁、通常 30 秒内自愈），由用户自行决定。该端点不放进高频轮询的 `/api/dsh/status`，
    避免每次轮询都去探测市场。
- 拉起后等 dsh 监听端口（上限 10 秒，端口开放后再等 2 秒确认没在装配阶段退出；
  进程已退出则立即判定失败，不等满上限）；**起不来就自动回滚**到旧目录并再次拉起，
  错误原样返回前端。实测本机 dsh 从进程启动到插件树装配完成约 2.4 秒。
- 停/起 dsh 走 `DshManager.Stop/Start` 的**精准终止**路径（PID / 进程组），
  不使用按进程名杀 —— `pkill -x MainThread|node-MainThread` 会命中该用户下所有 Node
  进程，包括市场正在跑的安装，杀完就留下陈旧写锁。启动 dsh 前与执行 `dsh plugin …`
  前会顺带清理「锁文件里的 PID 已不存在」的陈旧锁（`cleanStaleProfileLocks`），
  否则插件列表 / 安装会白等 120 秒再失败。
- 备份前缀是 `market-`，不会出现在「dsh 服务回滚」列表里；它由每日清理任务按 30 天回收。
- 只在「当前生效的那份由 server 包提供」时才允许更新。若市场已按 dsh 官方方式装进
  profile（`$DSH_HOME/profiles/web/node_modules/dshmarket`），控制台会显示
  「由 profile 提供，请在市场面板内更新」并禁用按钮 —— 那种情况下改 server 目录里那份
  不会生效（profile 条目优先）。`GET /api/market/info` 返回 scope / 目录 / 版本 / 原因，
  便于排查。

**与 server 包升级的关系**：升级 dsh 服务（或回滚 server 备份）会整目录替换 `server/`，
因此会覆盖掉控制台就地更新过的那份市场 —— 这是预期行为（新 server 包自带它构建时的
最新市场）。

---

## 更新下载：通路、重试与断点续传

三条更新链路（harness / dsh / 插件市场）共用同一个下载器（`backend/update.go`
的 `downloadToFile`），但**策略是分开的**（`downloadPlanFor`）：

| | harness / dsh（发布资产） | 插件市场（npm tarball） |
|---|---|---|
| 通路 | 代理（启用且探测可达时）+ 直连 | **只有直连，不走代理** |
| 重试 | 每条通路 2 次（最多 4 次） | 直连 2 次 |
| 断点续传 | 支持（HTTP Range） | **不支持**，每次从零下 |
| 暂停 | 支持 | **不支持**（按钮不显示，后端也忽略暂停请求） |

改动前请先读这一节：

- **已移除 GitHub 加速源**（gh-proxy 等）前缀分支：包一律从原始地址取。
- **失败归类**：所有通路都失败时返回 `errUpdateNetworkFailed`，状态里带
  `errorHint="network"`，前端用当前语言显示「请检查网络或代理设置后重试」；
  若其中任一次是败在本地磁盘（写不进去），则不给这个提示 —— 修网络没用。
- **断点续传**（仅发布资产）：半成品文件名按 `<kind>-<版本>.tar.gz` 固定，失败/暂停后
  重试都命中同一个文件，用 `Range: bytes=<offset>-` 续传（代理断在 60% 时直连接着下）。
  - 服务器回 `200`（忽略 Range，如某些代理会剥掉）→ 截断重写，本次从零开始；
  - 回 `416`（本地字节比远端还长，多半是远端换了资产）→ **删除半成品**并重新完整下载，
    绝不会把这份坏文件当成「已下完」；
  - 每次尝试结束都先 `fsync` 再关闭，保证半成品一定是「完整写入的连续前缀」。
- **暂停 vs 取消**（仅发布资产）：暂停（`POST /api/update/pause`）保留半成品并置
  `phase="paused"`，前端底部按钮变「继续下载」，再次调 `/api/update/download` 即续传；
  取消（`POST /api/update/cancel`）删除半成品；「删除更新包」（`/api/update/discard`）
  会连半成品一起清掉。注意半成品不跨进程重启保留（`pending/` 启动时整目录清理）。
- **市场不续传**：每次尝试前清掉残留、永远截断重写，失败后不留半成品
  （「要么完整拿到，要么什么都没有」），成功后再按 `dist.integrity` 校验。
- **超时策略**：下载客户端不设总超时（旧的 60 秒总超时会掐断大包/慢网），改为
  建连 30s、响应头 30s、**传输空闲 60s**（空闲看门狗，一有字节就重置）。
- 进度经 SSE 节流上报（每 500ms 或每 256KB），暂停/续传时字节数连续、不回跳。

---

## 启动期间的等待页与放行门禁

反代**在 dsh 启动之前**就已经监听配置的反代端口（`proxyPort`，见 `main.go`：`startProxy`
紧跟在 Admin socket 之后）。旧实现把反代放在“dsh 启动 → 换取 Cookie → 安装 node-pty”之后，
这期间访问反代端口既没有页面也没有响应，只能干等（首次启动装依赖时可能是几分钟）。

现在的顺序与门禁：

1. 反代开始监听，**先鉴权**：未登录的访客直接看到登录页（旧顺序是先判 dsh 是否就绪、
   后鉴权，导致启动期间根本无法登录）；`/_login`、`/_logout`、`/_ready` 是反代自留
   路径，不会转发给 dsh。
2. 已登录但 dsh 未就绪 → 等待页（`serveWaitingPage`）。页面**只显示一句话**「正在等待
   DeepSeek Harness 服务就绪」，并按 **1.5 秒**轮询 `GET /_ready`
   （JSON：`{phase, ready, detail}`）——换取凭据、安装 node-pty 等内部阶段不对外展示。
3. `/_ready` 报告 `ready=true` 时，页面立即 `location.replace` 跳到真正的 dsh 界面
   （不再靠整页刷新撞时机）；无 JS 客户端仍有 10 秒 `<meta refresh>` 兜底。

放行（把请求转发给 dsh）**只有一个判定入口**：`reverseProxy.state()`，三个条件全部满足
才放行，等待页与 `/_ready` 的返回值都来自同一处，避免“轮询说就绪 → 跳转 → 又是等待页”
的抖动：

| 条件 | 含义 | 不满足时的阶段 |
| --- | --- | --- |
| 启动流水线已收尾 | 不在 `starting / auth / deps` 阶段（收尾可能重启 dsh，提前放行会让界面随即失效） | `starting` / `auth` / `deps`（页面统一显示「等待服务就绪」） |
| dsh 端口已监听 | 配置的 `dshPort` 可连（反代的上游） | 进程在 → `starting`；进程不在 → `stopped` |
| 本代凭据已落定 | `DshManager.SessionSettled()`：本代 dsh 的 `dsh-auth-*` Cookie 已换取（或确认无需凭据） | `auth` |

`phase` 取值（`/_ready` 原样返回，便于排查；页面只区分「等待中」与「需要动手」两类）：

- 等待中（页面统一显示「正在等待 DeepSeek Harness 服务就绪」）：`starting`（拉起 dsh）、
  `auth`（换取凭据）、`deps`（安装/校正 node-pty），以及启动流水线之外的 `starting`
  （dsh 正在自重启/被拉起）。
- 需要用户动手（页面停转并给出对应指引）：`stopped`（dsh 未在运行）、`failed`
  （`dsh.Start()` 失败，`detail` 为原因）、`disabled`（`HARNESS_AUTOSTART=0`）。
- 等待超过 **30 秒**才出现“可能需要排查”的黄色提示（旧版固定在 10 秒后就报
  「启动失败」，属误报）。

改动这块时注意：

- 凡是**启动或重启 dsh** 的路径，都必须经 `captureDshSession` 换取凭据（它会调用
  `markSessionSettled`）。漏掉就不会标记落定，反代会一直停在等待页。
  `DshManager.Start()` 每次都会递增启动代号，使上一代凭据立即失效。
- 旧版 dsh 不打印 token（`WaitToken` 空等 15 秒超时）时同样算“落定”，最坏多显示
  15 秒等待页，不会永久卡住；反代随后可以不带 Cookie 转发（既有行为）。
- WebSocket 升级同样有门禁：未登录 → `401`；凭据未落定 → `503`；端口未就绪时保留
  10 秒容忍窗口（dsh 市场一键自重启期间客户端已在界面上，等它回来比立刻断开友好）。

---

## 主要流程

1. **启动** — 解析环境变量 → 读取配置 → 校验密码 → 启动 Admin socket →
   **启动反向代理**（TCP 根挂载 + 可选的 Unix Socket 子路径挂载；先监听、先鉴权，
   dsh 未就绪时给等待页）→ 自动启动 `dsh web --no-open --port <port>`
   （除非 `HARNESS_AUTOSTART=0`）。
2. **凭据交换** — 从 dsh 日志扫描一次性访问 token（`?token=`），用它访问一次
   dsh 地址，从 `Set-Cookie` 换取 `dsh-auth-*` 会话 Cookie，并标记本代凭据已落定
   （`markSessionSettled`，反代据此放行）。
3. **放行** — 三条件齐备（流水线收尾 + 端口就绪 + 凭据落定）后，反代携带该 Cookie
   把 dsh 反代到配置的反代端口；等待页轮询到 `ready` 即自动跳转。详见上文
   「启动期间的等待页与放行门禁」。
4. **node-pty** — 等待 `$HOME/.dsh/profiles/web` 目录生成后安装并 patch node-pty
   （仍在放行门禁内：此阶段即使端口已通也不放行）。
5. **自我更新（harness / dsh / 插件市场）** — 分「下载 → 安装」两步：下载可暂停
   （保留半成品，续传）/ 可取消（删除半成品），代理与直连各 2 次机会，详见上文
   「更新下载：通路、重试与断点续传」；包存放在 `TRIM_PKGVAR/backup/pending/`，
   安装成功后删除。三条分支收尾方式不同：
   - **dsh**：备份 → 替换 `server/` → 重启 dsh → 推送 `phase="done"`，前端据 SSE 收尾。
   - **插件市场**：校验 → 停 dsh → 备份并原子替换 `server/` 内那份 dshmarket → 自动拉起
     dsh 并换 token → 等就绪（失败自动回滚）→ 推送 `phase="done"`，详见上文
     「插件市场（dshmarket）的更新」。
   - **harness**：备份 → 替换自身二进制 → 停止 dsh → 删除更新包与临时目录 →
     `syscall.Exec` 换新映像。`exec` 之后本进程的任何代码都不再执行（`defer` 也不触发），
     因此**成功状态无法经 SSE 推送**——推送进程已消亡，前端改为轮询新进程上报的版本号
     判定就绪。同理，任何清理动作都必须放在 `exec` 之前显式完成。
   - 启动时会清理 `pending/` 下的残留更新包（`pending` 只存在于内存，进程重启即失效；
     上述 `exec` 路径尤其会留下孤儿文件）。
6. **退出** — 收到 `SIGINT/SIGTERM/SIGQUIT` 或 `stopCh` 后停止 dsh、移除 socket。

---

## 常见问题排查

- **Admin socket 已被占用** — 说明已有实例在运行，主进程会直接退出。
- **鉴权未启用** — 未设置 `password` 或密码强度校验失败时后端会打印警告，任何人可访问。
- **反代不带凭据** — 旧版 dsh 或日志未就绪导致未捕获 token 时，反代将不带 Cookie。
- **控制台启动期间打开反代地址只看到等待页** — 正常：dsh 还没就绪。页面只显示
  「正在等待 DeepSeek Harness 服务就绪」，就绪后自动跳转（内部阶段不对外展示）；等待
  超过 30 秒会出现“可能需要排查”的提示。若一直停在那里，多半是 dsh 起不来（不兼容的
  插件等），去控制台看日志。想手动确认状态可带登录 Cookie 请求 `GET /_ready`
  （返回 `{phase, ready, detail}`，`phase` 见上文「启动期间的等待页与放行门禁」）。
- **dsh 重启后页面白屏/接口报错** — dsh 正在重启（市场一键重启、更新 server 等），反代
  此时返回等待页；刷新即可，等待页也会在 dsh 回来后自动跳转。
- **CPU/内存读不到** — dsh 装插件自重启后 PID 变化，后端会自动在 `/proc` 中重新发现。
- **Firefox / Safari 打开会话只有「载入历史…」（且一直显示「深度求索中…」）** —
  非 V8 引擎的已知问题，开启设置页的「浏览器兼容模式」。若开启后仍无效，是浏览器
  仍在使用升级前的旧 bundle 缓存：**清除浏览器缓存或强制刷新（Ctrl+Shift+R）**；
  详见上文「浏览器兼容模式」一节。
- **改动了反代注入 / 前端后刷新看不到变化** — dsh 插件资源带一年期 `immutable` 强缓存
  且无 `ETag`，普通刷新不回源。清除浏览器缓存或强制刷新；必要时重启 dsh 使其 `rev` 变化。
- **`vue-tsc` 报 `baseUrl` 已废弃（TS5101）** — TypeScript 6 起 `baseUrl` 被标记废弃。
  本项目已移除 `baseUrl`，`paths` 改用相对本 tsconfig 的 `./src/*` 写法（`@/*` 别名
  在 vue-tsc 与 Vite 两侧均保持可用）。改回 `baseUrl` 会重新触发该报错。
- **前端类型检查命令** — `cd frontend && npm run typecheck`（即 `vue-tsc --noEmit`）。
  注意 TypeScript 需为 **6.x**：TS 7 移除了 `typescript/lib/tsc` 子路径导出，
  `vue-tsc` 3.x 依赖它，会直接以 `ERR_PACKAGE_PATH_NOT_EXPORTED` 崩溃。

---

## 许可

[MIT](LICENSE) © 2026 shuangji66

更多设计背景与说明见 [`genesis-DESIGN.md`](genesis-DESIGN.md)。