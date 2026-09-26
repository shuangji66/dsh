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
- **移动端模型 / 推理等级菜单** — 反代注入：修复 iPhone（WKWebView）上「模型 / 推理等级
  菜单能打开、点选项却毫无反应」；随「浏览器兼容模式」开关、只对触屏设备生效，桌面行为不变。
  （见下文「移动端模型 / 推理等级菜单（iOS）」一节）
- **登录鉴权** — 密码校验（≥8 位、大小写字母/数字/符号组合）、会话 Cookie、TTL
  有效期，以及访客管理（在线访客列表 / 踢出，SSE 实时推送）。**飞牛网关访问**
  （平台网关注入 `X-Trim-*` 身份头）跳过鉴权，端口访问照旧需要登录
  （见下文「飞牛网关访问」一节）。
- **Web 终端** — 基于 `creack/pty` + xterm.js 的交互式 bash 会话，通过 WebSocket 传输。
  **一个会话同一时刻只有一个操作端**：在新设备上打开会接管该会话，原设备收到提示后停在
  那里（不自动重连，点「重连」即可夺回）；浏览器断开只解挂载，会话继续运行并在重连后回放历史。
  移动端底部带**两页辅助键条**（功能键/方向键/常用符号 + 常见标点；Shift 为上档锁定，
  方向键同时变 Home/End/PageUp/PageDown），iPad 等大屏触屏同样显示（按触屏能力判定，
  不看屏幕宽度），平板档两页并排。触屏上可直接**上下拖动终端内容滚动回看**（xterm 自身
  不处理触摸，控制台自己把手指位移换算成行数），长按终端粘贴剪贴板；软键盘弹起时终端区
  与辅助键条一起贴到键盘上沿，拖终端不会把整页带着上滚。
- **目录授权** — 授权/查看已共享目录，并可将某已授权目录设为 dsh 主目录。
- **插件管理** — 列出 / 移除 / 重置 dsh web profile 的插件依赖。
- **快捷指令** — 持久化的终端快捷命令（`HARNESS_QUICK_CMDS_FILE`）；列表弹窗为紧凑卡片
  （新增/关闭为纯图标按钮，编辑/删除/上移/下移与命令名同一行）。
- **日志** — 查看 / 下载 / SSE 实时流式输出 dsh 与主进程日志（`HARNESS_LOG_FILE`）。
  后端日志统一走 `backend/logging.go` 这一个出口，分三级：`[INFO]`（白）/ `[WARN]`（黄）/
  `[ERROR]`（红）；终端（stdout 是 TTY）用 ANSI 着色，日志文件保持纯文本，由控制台日志页
  按 `[LEVEL]` 标记着色。连续重复的同一行只记一次、序列结束时汇总（如 `(previous message
  repeated 3566 times) …`）；基础的成功操作不记日志；dsh 子进程的输出原样透传（不改格式、
  不加前缀），在终端与控制台里都固定显示为黄色。更新类日志按目标分开打标签
  （`[harness]` / `[dsh]` / `[market]`），升级控制台与升级 dsh 服务的步骤不会混在一起。
- **更新管理** — 自动检测 harness 控制台 / dsh 服务 / 插件市场的新版本（每小时），
  下载走「代理 / 直连」两条通路（各 2 次机会，支持暂停与断点续传；是否先走代理由设置页
  的「代理更新」开关控制，不通则回退直连），
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
│   ├── logging.go           # 统一日志出口（等级 / 终端着色 / 重复抑制 / dsh 透传）
│   ├── admin.go             # Admin 管理 mux：SPA、API 路由、Unix socket 服务
│   ├── auth.go              # 登录鉴权（Cookie / HMAC / 密码校验）
│   ├── dsh.go               # DshManager：dsh 进程生命周期 / token 交换 / 状态 / 插件
│   ├── proxy.go             # 反向代理（携带 dsh 会话 Cookie）+ 等待页 / /_ready
│   ├── terminal.go          # WebSocket 交互式 PTY 终端（会话单挂载点）
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
        ├── composables/     # useTheme / useI18n / useConsolePrefs / useMobileLayout / useKeypadPage
        ├── serverapi/index.ts   # 运行时 baseurl 感知的 API / SSE 客户端
        ├── components/      # Toast / ConfirmDialog / 快捷指令 / 终端辅助键条 / 更新区
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
- `harness-build.yaml` — 手动触发，构建 harness 并发布到 GitHub Release
  （压缩包 + 同名 `.sha256` 校验文件）。
- `server-build.yaml` — 每 8 小时自动从 `@deepseek-ai/dsh` 打包 server（含
  `dshmarket` 依赖并注入 `PROFILE_TEMPLATES.web.bundles`），发布 Release
  （压缩包 + 同名 `.sha256` 校验文件）。

> 校验文件的命名就是「Release 资产名 + `.sha256` 后缀」
> （`harness-<版本>-<x86|arm>.tar.gz.sha256`、`server-<x86|arm>-<版本>.tar.gz.sha256`）：
> 控制台的更新链路按同一规则拼地址（`backend/update.go` 的 `checksumURL`），
> 改名会让校验静默退化成「不校验」。校验内容与失败处理见「更新包的 sha256 校验」一节。

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
| `HARNESS_DSH_DIAG` | 设为 `1`/`true`/`yes` 时给 dsh 页面注入**移动端诊断打点**（默认关闭，排查真机问题用；也可用页面 URL 的 `?dsh-diag=1` 只对单次访问开启，见「移动端模型 / 推理等级菜单（iOS）」一节） | 空 |
| `PROXY_PORT` | **已废弃**：反代监听端口改为设置页配置项（`config.json` 的 `proxyPort`，默认 `3079`），此环境变量不再生效 | — |
| `dsh_port` / `TARGET_PORT` | dsh web 端口 | `13080` |
| `proxy_mode` | 设为 `1` 时默认打开设置页的「代理dsh」（只影响 dsh 进程自身的出网） | `0` |
| `proxy_addr` | 代理地址（「代理dsh」与「代理更新」共用） | `http://127.0.0.1:7890` |
| `auth_mode` / `PROXY_AUTH` | 启用鉴权 | `true` |
| `password` | 登录密码 | 空 |
| `auth_ttl_hours` | 登录鉴权有效期（小时） | `4` |
| `TRIM_API_TOKEN` / `TRIM_APPNAME` | fnOS gateway 凭据 | — |

运行时配置（`config.json`）字段：`dshPort`、`proxyPort`、`proxyEnabled`、`proxyUpdate`、
`proxyAddr`、`authEnabled`、`password`、`authTTLHours`、`dshMemLimit`、`dshMemAuto`、
`homeDir`、`accessUrls`、`browserCompat`。可通过设置页修改并保存。

代理卡片有**两个相互独立的开关**，共用同一个代理地址：

- **代理dsh**（`proxyEnabled`，默认关闭）—— 只影响 dsh 进程自身的出网：开启时给 dsh
  下发 `http_proxy` / `https_proxy`（大小写与 `all_proxy` 一组，见 `dsh.go` 的 `buildEnv`），
  保存后需**重启 dsh** 才生效。
- **代理更新**（`proxyUpdate`，默认关闭）—— 只影响 harness 与 dsh 服务的**更新**
  （版本探测用的 HTTP 客户端 + 发布资产下载）：开启时更新优先走代理，代理地址探测不通
  或代理通路失败时**照旧回退直连**（下载通路是「代理在前、直连在后」）；
  插件市场的 tarball 下载始终直连，不受它影响。改完立即生效，无需重启任何进程。

node 堆内存上限（`dshMemLimit`，设置页「node 堆内存上限」）在**手动设置**
（`dshMemAuto` 关闭）时有两档阈值：低于 **800 MB** 输入框下黄字提醒（仍可保存），
低于 **500 MB** 红字报错并**阻止保存** —— 前端在 `store.save()` 里拦截
（`MEM_LIMIT_MIN_MB`），后端 `handleSaveSettings` 用同阈值 `minDshMemLimitMB` 兜底。
「自动设置」（默认）不受此限：上限由系统 node 的 `heap_size_limit` 决定，输入框只展示它。

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

## 飞牛网关访问（跳过登录鉴权）

平台网关（fnOS open-gateway）转发应用请求时会注入三个身份头，代表飞牛 OS 已完成
登录认证：

| 请求头 | 含义 |
| --- | --- |
| `X-Trim-Username` | 当前飞牛 OS 用户名 |
| `X-Trim-Userid` | 当前飞牛 OS 用户 UID（正整数） |
| `X-Trim-Isadmin` | 是否管理员（`1` / `true` / `yes`） |

对 dsh 服务而言，这类请求无需再输一遍控制台密码：

- **跳过登录鉴权** —— 网关那条线（Unix Socket 监听，`HARNESS_PROXY_SOCK`）上的请求
  只要带齐 `X-Trim-Username` + `X-Trim-Userid`，就不再被重定向到 `/_login`；
  WebSocket 升级共用同一份判定（不再回 `401`）。
- **等待界面照旧** —— 跳过鉴权不等于跳过就绪门禁：dsh 未就绪时照样是等待页，
  `/_ready` 轮询、dsh 会话凭据判定（`SessionSettled`）与转发行为都不变。
- **登录列表照旧记录** —— 网关请求不签 harness 会话 Cookie，因此按**飞牛用户
  （UID）+ 客户端 IP**标识：同一个人从不同环境（网络）访问飞牛时 IP 不同，各占一条
  记录，不会被合并刷新成同一条；同一环境的多次访问刷新同一条。列表里标注来源
  「网关访问」（端口访问一侧在 IP 前标注「端口访问」，两类来源一眼可分）、显示飞牛
  用户名与来源 IP（`X-Forwarded-For` / `X-Real-Ip`，都取不到时为空），不显示登录
  有效期，**不提供注销**（没有凭据可吊销，`DELETE /api/visitors`
  对这类条目直接拒绝）。这类条目**闲置**达到登录有效期（复用 `authTTLHours`，未配置
  时回退 4 小时，见 `gatewayVisitorIdleTTL`）后自动清除，避免列表长期堆积早已离开的
  用户。

**只有网关那条线认这三个头**（`reverseProxy.gatewayLine`，由 `newReverseProxyAt`
构造、`startProxySocket` 使用）。TCP 端口线（`proxyPort`，局域网可达）与回环地址
一概不认：请求头是客户端自己就能加的，端口线若认它，任何人加三个头即可绕过端口
鉴权。因此**局域网直连端口的伪造请求仍然要登录**；网关线上不带身份头、或身份头
不完整时也退回原有 Cookie 鉴权（例如本机进程直连 socket）。

设置页「启用登录鉴权」开关下方有对应提示：网关访问跳过鉴权，端口访问建议开启鉴权。

---

## 浏览器兼容模式（`browserCompat`）

**用途** — 一处开关、两处**只影响特定引擎**的兼容修复，默认**关闭**，在设置页
「node 堆内存上限」与「启用登录鉴权」之间切换；Chromium 内核（Chrome / Edge）开启无副作用：

1. **会话历史无法加载** —— **Firefox / Zen（SpiderMonkey）** 与 **Safari / 苹果设备
   （JavaScriptCore）** 上「会话历史一直显示『载入历史…』、且 AI 输出后无法恢复实时对话」；
2. **iPhone 上模型 / 推理等级菜单点选项没反应** —— WKWebView 在菜单内搬家焦点时
   `focusout` 的 `relatedTarget` 为 null 触发上游守卫误关菜单（见下文
   「移动端模型 / 推理等级菜单（iOS）」一节，该注入同样只在触屏设备上武装）。

下面第 1 项的成因与生效时机（含缓存）说明对两项都适用：第 1 项改写的是 bundle 字节，
升级 harness 后若浏览器仍缓存着旧字节，需要清缓存/强制刷新；第 2 项是 HTML 注入，普通刷新即生效。

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

## 移动端模型 / 推理等级菜单（iOS）

**症状** — iPhone（实测：飞牛 App 的 WKWebView，iOS 18.7，网关子路径访问）上点 composer 的
模型名：**菜单能打开**、菜单根面板的两行（「模型」「推理等级」）**也点得动**，但一进二级面板
点具体选项就**毫无反应**：菜单随即消失、模型名不变、网络里也没有任何 `session/selectModel`。
桌面（Firefox/Windows，同一个子路径 URL）鼠标点击一切正常——包括同一会话、同一时刻。

### 问题成因

dsh 0.1.7 的模型座位（`dsh-client-ui-model-selection` 的 `ModelSelect`）自己实现了两级菜单，
并在根节点上用 `onBlur` 关菜单：

```js
const onBlur = (event) => {
  if (event.relatedTarget instanceof Node &&
      (rootRef.current?.contains(event.relatedTarget) === true ||
       menuRef.current?.contains(event.relatedTarget) === true)) return;
  close();
};
```

它假定「失焦目标一定是个 Node」。**桌面成立**：点选项时焦点落到那个选项上，`relatedTarget`
就是它、且在 `menuRef` 里 → 守卫放行 → 菜单留着 → `click` 派发给选项 → 选择生效。

**iOS WebKit 不成立**：菜单内按钮之间的焦点搬家，`focusout` 的 `relatedTarget` 是 **null**
（真机打点实测：菜单里已聚焦的选项 `focusout` 事件 `relatedTarget === null`）。守卫于是越过
`return` 直接 `close()`，**菜单在 `mousedown` 之后、`click` 之前被卸载** → 选项的 `click`
没有目标、React 的 `onClick` 不跑，也就永远不会发出 `session/selectModel`。

真机数据（诊断打点，模型座位内事件）可直接对上：

| 观察 | 数据 |
| --- | --- |
| 二级面板选项上的事件 | `pointerdown → touchstart → pointerup → mousedown`（齐全）→ **没有 `click`** |
| 紧跟着的 focusout | `target = BUTTON._7KE1Ra_option _7KE1Ra_selected, relatedTarget = null` |
| 同一时刻 | `[role=menu]` 消失（菜单被卸载）；座位本身 `disabled=false`、座位中心命中就是它、`defaultPrevented=false` |
| 根面板两行 | `click` 正常派发（那两行是进入二级面板的唯一入口，所以"能钻进去但点不动"） |

复现与回归（无需真机）：在 Chromium/WebKit 里把「菜单内 focus 搬家」按 iOS 的方式模拟
（在菜单内 `mousedown` 时对手上已聚焦的菜单按钮调用 `blur()` —— 程序化 blur 的
`relatedTarget` 同样是 null），未修复时选项点击 0 次选择、修复后 1 次。

### 修复方式

`bootstrapScript` 第 6 段（随设置页的**「浏览器兼容模式」**开关 + 触屏判定一起武装）：触屏上把**模型座位自己那个菜单内部**的 `focusout` 在捕获阶段
拦掉传播——React 的委托监听挂在更低的容器上，拦在最外层就让它收不到这次失焦，菜单不会被
误关，随后的 `click` 正常落到选项上。边界刻意收窄：

- 只认模型座位（`[data-slot="conversation.input.model"]`）且菜单确实开着（`aria-expanded="true"`）；
- 菜单用触发器的 `aria-controls` 定位（规范属性，不依赖 dsh 的样式哈希），命令 / 权限 /
  会话行等**其他菜单一律不碰**；
- 只拦 `target` 在该菜单内的 `focusout`：编辑面与菜单以外的失焦照旧（「点菜单外关菜单」走的是
  `mousedown`，不受影响，实测仍能关闭）；
- 只在触屏设备上武装（`navigator.maxTouchPoints` / `ontouchstart`），桌面行为与官方 dsh 完全一致。

生效方式是 **HTML 注入**（不是改写 bundle 字节），HTML 每次刷新都回源，因此**普通刷新即生效**，
不受 dsh 插件 bundle 的 `immutable` 强缓存影响（这点与开关里第 1 项不同）。页面可用
`window.__DSH_MODEL_MENU_FOCUS_GUARD__ === true` 确认守卫已武装（开关关闭时该标记不会出现）。

### 真机诊断（默认关闭）

这类只在真机上出现的问题（事件被吞、元素在两次事件之间被卸载）靠"看有没有发出请求"是查不出来的。
反代内置了一套默认关闭的打点：环境变量 `HARNESS_DSH_DIAG=1`（进程级）或页面 URL 带
`?dsh-diag=1`（访问级，例如手机上打开 `https://<网关>/app/Harness/dsh/?dsh-diag=1`）即注入。
它把「模型座位与菜单」相关的指针/焦点/菜单出现消失/座位节点替换/JS 报错，以
`<prefix>/dsh-diag/<事件>/<分片>/<数据>` 的 **URL path** 形式打回本站（dsh 回 404，无副作用），
落进平台网关 / nginx 的 `access.log`——不需要新开后端接口就能取回一个真机客户端的现场。
解读方式与判定要点见 `AGENTS.md`「真机诊断打点」一节。

### 与移动端页面插件的关系

**不是** `dsh-web-mobile-fix` / `dsh-web-mobile` 之类插件造成的，证据：

- `dsh-web-mobile-fix` 的捕获期 `click` 处理只作用于「侧边栏展开时落在中栏」的点击，而且它的
  第一条豁免就是 `[role="menu"]` 子树；模型菜单是 **portal 到 `document.body`** 的，既不在中栏
  也不在侧边栏列里——代码路径根本到不了。
- 真机上模型菜单里的事件 `defaultPrevented` 全是 `false`，且钩子注册在 `<head>`（比任何插件都
  早），没有任何插件拦掉这些事件；根面板两行的 `click` 也正常派发。
- 另一台设备（桌面 Firefox）、同一个子路径 URL、同一会话正常工作。
- 该实例上 `dsh-web-mobile` 本来就是被 `cordis.patch.yml` 停用的（其 client bundle 根本没加载）。

顺带记录一个**独立**的移动端小坑（与本缺陷无关、属插件设计）：`dsh-web-mobile-fix` 在 ≤700px
把展开的侧边栏做成浮层，并在捕获期吃掉「侧边栏展开时落在中栏的点击」用于收起侧边栏——因此
侧边栏展开时点模型名只会收起侧边栏，需要再点一次。

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
  **自动拉起 dsh 并重新换取会话 token**（`startDshCaptured`）。这份备份包只在本次安装里
  当回滚兜底，**收尾即删**（见下文「备份包的留存规则」）。
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
- 备份前缀是 `market-`，不会出现在「dsh 服务回滚」列表里；这份包收尾即删，历史上留下的
  那些由每日清理任务按 30 天回收。
- 只在「当前生效的那份由 server 包提供」时才允许更新。若市场已按 dsh 官方方式装进
  profile（`$DSH_HOME/profiles/web/node_modules/dshmarket`），控制台会显示
  「由 profile 提供，请在市场面板内更新」并禁用按钮 —— 那种情况下改 server 目录里那份
  不会生效（profile 条目优先）。`GET /api/market/info` 返回 scope / 目录 / 版本 / 原因，
  便于排查。

**与 server 包升级的关系**：升级 dsh 服务（或回滚 server 备份）会整目录替换 `server/`，
因此会覆盖掉控制台就地更新过的那份市场 —— 这是预期行为（新 server 包自带它构建时的
最新市场）。

## 备份包的留存规则

`TRIM_PKGVAR/backup/` 下按 `<类型>-<版本>-<时间戳>.tar.gz` 命名。**留不留只看「有没有回滚
入口」**（`update.go` 的 `removeUnusedBackup`）：

| 类型 | 谁生成 | 留存 | 原因 |
|---|---|---|---|
| `server-*` | 更新 dsh 服务（`applyServer`） | **保留** | 概览页有「dsh 服务回滚」，回滚后还要能再回滚到别的版本 |
| `dsh-data-*` | 目录页「备份」（用户主动） | **保留** | 用户的数据备份，只能手动删；不参与自动清理 |
| `harness-*` | 更新 harness 控制台（`applyHarness`） | **收尾即删** | 控制台没有回滚入口，包不会被任何代码读取 |
| `market-*` | 更新插件市场（`swapMarketDir`） | **收尾即删** | 同上；回滚兜底只用本次安装内的旧目录，包一旦用不到就删 |

唯一的例外是「回滚本身失败」：市场那次如果连备份包都解压不回去（目标目录已损坏），包会
被刻意留下 —— 此刻它可能是旧版本唯一的副本。harness / market 老版本留下的包仍由每日清理
任务按 30 天回收。

**新增更新分支时沿用同一条规则**：没有回滚入口就别把备份包留在盘上。

---

## 更新下载：通路、重试与断点续传

三条更新链路（harness / dsh / 插件市场）共用同一个下载器（`backend/update.go`
的 `downloadToFile`），但**策略是分开的**（`downloadPlanFor`）：

| | harness / dsh（发布资产） | 插件市场（npm tarball） |
|---|---|---|
| 通路 | 代理（「代理更新」开启且探测可达时）+ 直连 | **只有直连，不走代理** |
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

## 更新包的 sha256 校验

`harness-build.yaml` / `server-build.yaml` 打包后额外生成**同名 `.sha256` 文件**
（`sha256sum` 的标准输出：`<64 位十六进制>  <文件名>`），与包一并上传到 Release 资产。

harness 控制台与 dsh 服务的自我更新会一并取回它并用**实际下载到的字节**复核摘要
（`backend/update.go` 的 `releaseChecksum` / `verifyFileSHA256`）：

- **取回方式**：与包相同的通路序列（代理 → 直连，各 2 次机会）；校验文件只有几十字节，
  不复用 `downloadToFile` 那套续传/进度/空闲看门狗，单次请求整体限时 30 秒、正文限长截断。
  用户取消/暂停后不再继续重试。
- **校验过程静默**：取校验文件、算摘要都**不写更新状态、不进弹窗**（弹窗只显示下载进度），
  校验成功也不记日志 —— 只有失败才被推给弹窗（`UpdateSection.vue` 的失败提示区）。
- **摘要不符即失败**：错误为「更新包 sha256 校验失败：…请重新下载」，并把那份包**删掉**
  （留着会让下次续传从错误位置接、或直接撞 416 白跑一轮），不进入「已下载待安装」。
- **安装前再复核一次**：摘要随「待安装包」记下（`PendingUpdate.SHA256`），安装阶段解压之前
  重算一次 —— 这份文件要跨「下载 → 安装」两步留在盘上，期间可能被截断或替换
  （与插件市场的 `dist.integrity` 复核同理）。不通过则拒绝安装，包保留由用户决定是否删除重下。
- **校验文件缺失时不阻塞更新**：本次改动之前发布的那批资产没有 `.sha256`，取不到、
  404、内容无法解析都只记一行 WARN，退化为不校验并继续更新 —— 不让用户卡在「无法更新」。
  这也是为什么**校验成功路径必须保持安静**：否则老资产的每次更新都会多出一串噪音日志。

> 插件市场（dshmarket）走的是 npm registry 的 `dist.integrity` / `dist.shasum`（元数据自带摘要，
> 两个都没有则拒绝安装），见「插件市场（dshmarket）的更新」一节。

---

## 启动期间的等待页与放行门禁

反代**在 dsh 启动之前**就已经监听配置的反代端口（`proxyPort`，见 `main.go`：`startProxy`
紧跟在 Admin socket 之后）。旧实现把反代放在“dsh 启动 → 换取 Cookie → 安装 node-pty”之后，
这期间访问反代端口既没有页面也没有响应，只能干等（首次启动装依赖时可能是几分钟）。

现在的顺序与门禁：

1. 反代开始监听，**先鉴权**：未登录的访客直接看到登录页（旧顺序是先判 dsh 是否就绪、
   后鉴权，导致启动期间根本无法登录）；`/_login`、`/_logout`、`/_ready` 是反代自留
   路径，不会转发给 dsh。唯一例外是**飞牛网关访问**（网关那条线上带 `X-Trim-*`
   身份头，见「飞牛网关访问」一节），它跳过登录鉴权、直接进入下一步。
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
   - **dsh**：备份 → 替换 `server/` → 重启 dsh → 推送 `phase="done"`，前端据 SSE 收尾；
     备份包**保留**（概览页「dsh 服务回滚」要用）。
   - **插件市场**：校验 → 停 dsh → 备份并原子替换 `server/` 内那份 dshmarket → 自动拉起
     dsh 并换 token → 等就绪（失败自动回滚）→ 推送 `phase="done"`，详见上文
     「插件市场（dshmarket）的更新」；备份包收尾即删。
   - **harness**：备份 → 替换自身二进制 → 停止 dsh → 删除备份包、更新包与临时目录 →
     `syscall.Exec` 换新映像。`exec` 之后本进程的任何代码都不再执行（`defer` 也不触发），
     因此**成功状态无法经 SSE 推送**——推送进程已消亡，前端改为轮询新进程上报的版本号
     判定就绪。同理，任何清理动作都必须放在 `exec` 之前显式完成。
   - 三条分支的备份包留不留，统一按上文「备份包的留存规则」（没有回滚入口就不留）。
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