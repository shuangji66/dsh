# DeepSeek Harness 控制台

一个自包含的 **DeepSeek Harness (dsh) 管理控制台**。它在 fnOS（TRIM 平台）上负责
拉起并守护 `dsh web` 进程，并通过一个内嵌的 Vue 管理界面提供：
概览、设置、目录授权、插件管理、Web 终端、日志查看与更新管理等功能。

本仓库（`shuangji66/dsh`）即 DeepSeek Harness 控制台的后端 + 前端源码。

> 它**不是** `dsh` 本体，而是管理 `dsh` 的“控制台”。dsh 服务本身是官方的
> `@deepseek-ai/dsh` npm 包，由控制台按需从设置页选中的 npm 镜像源安装到
> `${TRIM_PKGVAR}/server/<版本>/`（不再随 fpk 预置、也不再走 GitHub Release 压缩包）。

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
- **登录鉴权** — 密码校验（≥8 位，含字母/数字/标点）、会话 Cookie、TTL
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
- **目录授权** — 授权/查看已共享目录（主目录固定为应用自己的 shares 目录，不可切换）。
  飞牛 App **1.38.0 之前**（或读不到 App 版本）的内置 WebView 不走宿主 `pickUserFile`
  桥接（会按共享授权处理并报鉴权异常），而是把 WebView 当作独立 Web，同窗口打开平台的
  网页授权页 `/app-auth/pick-user-file`，完成后由平台跳回本页；1.38.0 起官方修复了
  原生选择器，直接用宿主选择目录。
- **插件管理** — 列出 / 启用 / 停用 / 卸载 / 重置 dsh web profile 的插件依赖（插件名点击即复制）；
  顶栏「安装」按 dsh 官方方式装插件：包名、包名@版本、Git 仓库地址、本地绝对路径（目录或
  `.tgz`），或直接粘贴整段 `dsh plugin add …` 命令；安装是后台任务（关掉弹窗继续跑，进度
  经状态接口轮询），成功后按包类型提示「重启 dsh 生效」，镜像源固定用设置页选中的那一个；
  卡片右上角的信息按钮按 dsh 官方顺序显示插件详情（`locale` 的 `meta.detail` 优先、README
  兜底，跟随界面语言），点开才读、本次会话内临时缓存，没有文档时显示「无内容」；
  启停按官方 dsh 的 `dsh.profile.bundles` 选择（停用保留依赖，不写插件市场的
  `.dsh-market/state.json`），只有声明了 `dsh.bundle` 的包才给开关；启用前过官方的兼容性
  栅栏（`engines.dsh` / `@deepseek-ai/dsh*` peer 范围，`compatibility.json` 里的放行记录
  优先），安装/更新正在进行时写清单会回 409，避免与 pnpm 抢同一份 `package.json`。
- **弹窗交互** — 打开任一弹窗（更新 / dsh 版本 / 插件市场 / 备份进度 / 停止重启确认…）都会
  **锁定背景页面滚动**，移动端不会把弹窗背后的控制台页面一起拖走；叠加的二次确认各加一层锁，
  全部关闭才解锁。弹窗内的长信息按「组合」换行 —— 例如 dsh 版本行在窄屏下**版本号独占一行**，
  dist-tags 与「已安装 / 使用中」整组落到下一行（不会挤在版本号旁边，也不会从连字符处断开）。
- **移动端滚动区** — 页面滚动区止于底部导航栏上沿（滚动条不会滑进/压在 fixed 底栏上），
  底栏恒定贴底不动；软键盘弹起时底栏隐去，滚动区与终端页一起贴到键盘上沿。
- **快捷指令** — 持久化的终端快捷命令（数据目录下的 `quickcmds.json`）；列表弹窗为紧凑卡片
  （新增/关闭为纯图标按钮，编辑/删除/上移/下移与命令名同一行）。**命令内容加密落盘**
  （AES-256-GCM，密钥为 `master.key` 或 `HARNESS_MASTER_KEY`）：快捷指令里常放
  `mysql -uroot -p<密码>`、带 token 的 curl 这类东西，拿到磁盘快照/备份的人不该顺手读到它们；
  旧版本留在盘上的明文会在启动时（以及第一次打开弹窗时）自动加密重写，名称仍是明文以便排查。
  加载失败时弹窗给「重试」而不是显示「暂无快捷指令」—— 后端是整表保存，让用户在不可信的
  空列表上新增会把服务端已有的指令整表覆盖掉。
- **日志** — 查看 / 下载 / SSE 实时流式输出 dsh 与主进程日志（数据目录下的 `harness.log`）。
  后端日志统一走 `backend/logging.go` 这一个出口，分三级：`[INFO]`（白）/ `[WARN]`（黄）/
  `[ERROR]`（红）；终端（stdout 是 TTY）用 ANSI 着色，日志文件保持纯文本，由控制台日志页
  按 `[LEVEL]` 标记着色。连续重复的同一行只记一次、序列结束时汇总（如 `(previous message
  repeated 3566 times) …`）；基础的成功操作不记日志；dsh 子进程的输出原样透传（不改格式、
  不加前缀），在终端与控制台里都固定显示为黄色。更新类日志按目标分开打标签
  （`[dsh]` / `[market]` / `[advanced]`），升级 dsh 服务与两个受管插件的步骤不会混在一起。
- **dsh 服务多版本管理** — dsh 服务**不再内置在 fpk 安装包里**，而是按需从设置页选中的
  npm 镜像源（默认阿里云，可选腾讯云 / 华为云 / 官方源）安装官方包到 `${TRIM_PKGVAR}/server/<版本>/`，
  可同时装多个版本并自由切换（切换 = 用该版本重启 dsh）；下载时支持取消（会清除安装
  目录与下载缓存），安装成功的版本可删除；未安装任何版本时控制台显示「未安装」。
  （见下文「dsh 服务的多版本管理」一节）
- **更新管理** — 自动检测 dsh 服务 / 插件市场 / 增强插件的新版本（每小时）；三者的
  安装与更新都以设置页选中的 npm 镜像源为准，不走 GitHub，默认不碰官方源
  （只有用户主动选中时才用），且只用所选源、失败即报错并提示换源。
  **控制台自身不提供就地自我更新**：升级只走 fpk 安装包（见下文「控制台版本与升级方式」）。
  另有 dsh 数据备份 / 恢复（目录页）：备份在服务端异步执行，弹窗显示进度、可取消，
  关掉弹窗不会终止备份（见下文「dsh 数据备份」一节）。
- **node-pty 自动校正** — 主进程启动后确保 profile 的 `pnpm-workspace.yaml` 把
  `node-pty` 固定到 `1.2.0-beta.15`，并清理旧的 patch 产物（只有发现旧 patch 时才
  `pnpm install` 并重启 dsh）。

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
├── build.sh                 # 同一流水线的构建脚本（dev 也 strip）
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
│   ├── update.go            # 更新检测（dsh / 插件市场 / 增强插件）+ dsh 数据备份/恢复
│   ├── server.go            # dsh 服务多版本：镜像源列表 / npm 安装 / 取消 / 删除 / 切换
│   ├── market.go            # 受管插件（dshmarket / dsh-fnos-advanced）：检测 / 安装 / 更新 / 卸载
│   ├── install.go           # node-pty 版本固定与旧 patch 清理
│   ├── quickcmds.go         # 终端快捷指令持久化 API（命令内容加密落盘 + 旧明文迁移）
│   ├── dsh_env.go           # 自定义环境变量：校验 / 落盘（变量值加密，dsh-env.json）/ buildEnv 注入
│   ├── secret.go            # 落盘加密：AES-256-GCM 信封 + 主密钥（master.key / HARNESS_MASTER_KEY）
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
        ├── composables/     # useTheme / useI18n / useMobileLayout / useKeypadPage
        ├── serverapi/index.ts   # 运行时 baseurl 感知的 API / SSE 客户端
        ├── components/      # Toast / ConfirmDialog / 快捷指令 / 终端辅助键条 / 版本与更新区
        └── views/           # 概览 / 设置 / 目录 / 插件 / 终端 / 日志
```

---

## 构建

> 前置：Go ≥ 1.27、Node.js ≥ 24。

构建会把前端 `dist` 拷入 `backend/embed`，再用 `//go:embed` 把静态资源打进行内嵌进
Go 二进制，最终生成**单文件自包含**的 `backend/harness`。

```bash
# 开发构建（make 默认非 stripped）
make

# Release 构建（strip + 外部链接），可指定版本号
make release V=1.0.1

# 清理构建产物
make clean
```

两者走同一条流水线（前端 `dist` → `embed` → Go 二进制），都内置 node24 的 PATH、都支持
覆盖版本号。注意 `make` 的 dev 默认不 strip，而 `./build.sh` 的 dev 也带 `-s -w`。

```bash
./build.sh               # dev
./build.sh release 1.0.1 # release + 指定版本
```

### 不含终端功能的构建

终端页、PTY 会话与快捷指令可以在编译期整体剔除：产物里不会留下终端接口与 PTY 逻辑，
前端连 xterm 都不会打包（侧边栏入口、旧 `/terminal` 路径一并收起）。

```bash
TERMINAL=0 make                  # 等价于 make noterminal
TERMINAL=0 make release V=1.0.1
TERMINAL=0 ./build.sh            # build.sh 同样认这个开关

./build-noterminal.sh            # 一次交叉编译 amd64 + arm64 发布产物
```

两半是同一个开关：Go 侧 `-tags noterminal`，前端侧 `VITE_TERMINAL=0`。直接调
`go build` / `vite build` 时需要自己给这两个参数。

`./build-noterminal.sh` 固定按 `linux/amd64`、`linux/arm64` 编译（`CGO_ENABLED=0`），
产物为 `dist/harness-amd64` 与 `dist/harness-arm64`；第一个参数可换输出目录，
`VERSION=1.0.1` 可覆盖版本号。

GitHub Actions（`.github/workflows/`）现在只剩两项：

- `frontend-check.yaml` — 前端 push / PR 时跑 typecheck、lint、单测，并分别用默认参数与
  `VITE_TERMINAL=0` 构建一次（后者断言 xterm 没有进包）。
- `dsh-fnos-advanced-publish.yaml` — 手动把增强插件发布到 npm。

控制台二进制与 dsh server 包都不再由 CI 构建：旧的 `build.yml` / `server-build.yaml`
已删除；升级只走 fpk（见「控制台版本与升级方式」），dsh 由控制台从 npm 镜像源安装
（见「dsh 服务的多版本管理」）。

> 控制台不再下载、也不再生成任何「更新包」发布资产（自我更新已整体移除）。
>
> 本地备份（`dsh-data-*`）恒为 `.tar.gz`（`tgzDirAs`），解压直接走 gz 解压器。

### 归档解压：只有 gzip 一条通路

控制台现在只需要解 `.tar.gz`：`dsh-data-*` 数据备份（`extractTarGz`）。tar 遍历、
路径越界与软链逃逸防护都在 `extractTar` 里。恢复路径直接调用 `extractTarGz`；
`extractArchive` 只是等价包装（当前没有调用者）。

> 旧版本为 dsh server 的 `.tar.xz` 发布资产准备过「外部 `xz` 优先、纯 Go 兜底」两条
> 解码通路；随着 server 压缩包这条链路整体移除，xz 相关代码与依赖已经删除
> （设备上不再需要安装 `xz-utils`）。

---

## 运行 / 环境变量

二进制经 fnOS 平台以 `/var/apps/Harness` 部署，监听 **Admin Unix Socket**
（默认 `<appDest>/app.sock`），前端由 nginx 反代到该 socket 的 baseurl 前缀。
对外只有**一个网关入口**（`gatewayPrefix=/app/Harness`、socket `app.sock`）：
控制台在 `/app/Harness/…`，dsh 是它下面的 `/app/Harness/dsh/…` 子路径（由控制台
自己反代，见下文「子路径部署」）。另有 TCP 端口（配置项 `proxyPort`，默认 `3079`，
占据站点根）作为不走网关的直连入口。

| 环境变量 | 说明 | 默认 |
| --- | --- | --- |
| `HARNESS_DATA_DIR` | **统一的后端数据目录**：日志、PID、配置、会话密钥、终端会话镜像与 dsh 数据备份全部在其下派生（见下表）。单独的路径环境变量已移除 —— 要换位置只改这一个 | `$TRIM_PKGVAR`（缺失时退到当前工作目录） |
| `HARNESS_ADMIN_SOCK` | Admin Unix socket 路径 | `$TRIM_APPDEST/app.sock` |
| `HARNESS_ADMIN_BASEURL` | 前端资源 baseurl 前缀 | `/app/Harness` |
| `HARNESS_AUTOSTART` | 设为 `0` 时不自动启动 dsh | `1` |
| `HARNESS_MASTER_KEY` | 落盘加密的**主密钥**（32 字节 base64 或 hex）：加密 `quickcmds.json` 的命令内容、`dsh-env.json` 的自定义环境变量与 `dsh-accounts.json` 账号库。给了就不再读数据目录里的 `master.key`；**解不出来时直接失败**，不会静默回退到文件。想让数据目录的备份/卷拷贝里没有可用密钥，就由启动脚本注入它 | 空（用 `<数据目录>/master.key`） |
| `DSH_HOME` | dsh 的**数据根**（dsh 自己的解析规则：`$DSH_HOME` > `~/.dsh`；平台启动脚本会注入）。控制台只按它定位凭据文件 `<DSH_HOME>/.credentials.yaml` —— 位置写错会让登录「写入成功但 dsh 不认」；其余 `<HOME>/.dsh/...`（profiles、备份）仍按 `HOME` 拼 | `<HOME>/.dsh` |
| `PROXY_PORT` | **已废弃**：反代监听端口改为设置页配置项（`config.json` 的 `proxyPort`，默认 `3079`），此环境变量不再生效 | — |
| `dsh_port` / `TARGET_PORT` | dsh web 端口 | `13080` |
| `proxy_mode` | 设为 `1` 时默认打开设置页的「代理dsh」（只影响 dsh 进程自身的出网） | `0` |
| `proxy_addr` | 代理地址（「代理dsh」使用） | `http://127.0.0.1:7890` |
| `auth_mode` / `PROXY_AUTH` | 启用鉴权 | `true` |
| `password` | 登录密码 | 空 |
| `auth_ttl_hours` | 登录鉴权有效期（小时） | `4` |
| `TRIM_API_TOKEN` / `TRIM_APPNAME` | fnOS gateway 凭据 | — |

数据目录下的内容（`backend/config.go` 的 `loadRuntimeEnv` 统一派生；要换位置改
`HARNESS_DATA_DIR` 即可，不要再新增单独的路径环境变量）：

| 路径 | 内容 |
| --- | --- |
| `<数据目录>/config.json` | 运行时配置（设置页保存的目标） |
| `<数据目录>/harness.log` | 日志落盘文件（进程停止时删除） |
| `<数据目录>/harness.pid` | harness 控制台自身 PID（恒不变） |
| `<数据目录>/dsh.pid` | dsh 服务实时 PID（随 dsh 启动/自重启刷新，dsh 停止时移除） |
| `<数据目录>/session.key` | 会话 cookie 签名密钥（0600，跨重启保住登录态） |
| `<数据目录>/quickcmds.json` | 终端快捷指令持久化文件（0600；命令内容是密文，名称/是否自动回车仍是明文） |
| `<数据目录>/dsh-accounts.json` | 控制台的 **DeepSeek 账号库**（0600；整份列表是一个密文信封，多账号的 token 都在里面） |
| `<数据目录>/dsh-env.json` | 设置页「自定义环境变量」（0600；变量名明文、变量值逐条密文，主密钥同上） |
| `<数据目录>/master.key` | **落盘加密的主密钥**（0600，缺失时自动生成）：`quickcmds.json` 的命令内容、`dsh-env.json` 的环境变量值与 `dsh-accounts.json` 都是它加密的；删掉它 = 这三份密文再也解不开（想避免密钥与密文同放，用 `HARNESS_MASTER_KEY` 注入） |
| `<数据目录>/terminal-sessions/` | 终端会话历史临时镜像（停止时整目录清除） |
| `<数据目录>/backup/` | dsh 数据备份包（`dsh-data-*`，用户主动创建）；老版本自我更新遗留的 `pending/` 在启动时自动清理 |
| `<数据目录>/server/<版本>/` | 每个已安装的 dsh 版本（`npm install --prefix` 装出来的目录，可用版本就在 `node_modules/.bin/dsh`） |
| `<数据目录>/npm-cache/` | 控制台安装 dsh 版本时使用的 npm 缓存（取消下载会连同安装目录一起清除） |

**例外**：pnpm 目录不在数据目录下，仍由 `PNPM_HOME` 单独指定（dsh 进程与终端都
按它设置环境变量）。

dsh 进程还会拿到 **`DSH_AGENTS_HOME=$HOME/.agents`**（共享 agent 配置根：技能 /
全局指令等跨 agent 工具共享的配置，见 `@deepseek-ai/dsh-home-paths` 的
`resolveAgentsHome`）。它由 `DshManager.buildEnv` 显式下发，免得 dsh 用
`os.homedir()` 兜底而跑到平台共享目录之外；目录本身由**控制台启动时检测并创建**
（`ensureAgentsHome`，创建成功不记日志），dsh 启动时不会自建。

`buildEnv` 还把 **`SHELL` 固定为 `/bin/bash`**，不继承平台给服务账号设的
`/usr/sbin/nologin`：dsh 的内置终端按 `process.env.SHELL` 解析默认 shell
（`@deepseek-ai/dsh-subprocess-local` 的 `terminalEnvironment` →
`dsh-api-terminal-controller` 的 `resolveShell`），继承 nologin 会让 dsh 的终端页
直接跑成 "This account is currently not available."。控制台自己的终端同样固定
`/bin/bash`（见 `terminal.go` 的 `terminalEnv`），两边保持一致。

运行时配置（`config.json`）字段：`dshPort`、`proxyPort`、`proxyEnabled`、
`proxyAddr`、`npmRegistry`、`authEnabled`、`password`、`authTTLHours`、`dshMemLimit`、
`dshMemAuto`、`dshEnvEnabled`、`dshVersion`。其中 `dshVersion` 是
**当前选中的 dsh 版本**（即 `${TRIM_PKGVAR}/server/` 下的目录名，空 = 未安装），由概览页的
版本列表写入，设置页不暴露；`npmRegistry` 是设置页「npm 镜像源」的取值（`aliyun` /
`tencent` / `huawei` / `npmjs`，默认 `aliyun`），dsh 服务与两个受管插件的安装/更新/版本检测
都只用它。控制台只使用平台通过环境变量（`PATH`）提供的 **node24**，
不再检测或切换 node 版本（`nodeVersion` 配置项已移除）。旧配置里遗留的 `browserCompat`
（已移除的「浏览器兼容模式」）现在只是一个未知字段，加载时被忽略。

「加速设置」卡片有两项：

- **代理dsh**（`proxyEnabled`，默认关闭）—— 只影响 dsh 进程自身的出网：开启时给 dsh
  下发 `http_proxy` / `https_proxy`（大小写与 `all_proxy` 一组，见 `dsh.go` 的 `buildEnv`），
  保存后需**重启 dsh** 才生效。控制台自身的版本检测、dsh 版本安装与插件安装都不走它。
- **npm 镜像源**（`npmRegistry`，默认阿里云）—— dsh 服务与两个受管插件（市场 / 增强插件）的
  安装、更新和版本检测都**只用所选源**，失败会报错并提示换源；它需要点「保存配置」才落盘生效
  （npm 安装不走代理，所以这一项与上面的代理开关无关）。

「运行环境」卡片里是 node 堆内存上限与「**自定义环境变量**」开关（`dshEnvEnabled`，默认关闭）。
开关右侧的齿轮打开编辑弹窗；变量表保存在 `<数据目录>/dsh-env.json`（变量名明文便于排查，
变量值逐条加密落盘，主密钥与快捷指令/账号库同一把）。弹窗里的「保存」只负责把变量表落盘，**还要点设置页
的「保存配置」并重启 dsh** 才会由 `buildEnv` 注入 dsh 进程。变量名必须是 POSIX 标识符，且不能
是控制台保留名（`PATH` / `HOME` / `SHELL` / `DSH_HOME` / `DSH_AGENTS_HOME` / `DSH_WEB_URL` /
`NODE_OPTIONS` / 代理变量 / `TRIM_APPNAME`、`TRIM_PKGVAR`、`TRIM_APPDEST` /
`HARNESS_DATA_DIR` / `HARNESS_MASTER_KEY` / `DSH_SUBPROCESS_*` 前缀等，完整名单由后端下发）。

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

## 子路径部署：控制台入口下的 `/dsh` 挂载

dsh 不作为独立的网关入口暴露。平台网关（fnOS open-gateway）只看到控制台这一个
入口前缀（`gatewayPrefix=/app/Harness`、socket `app.sock`），dsh 挂在它下面的
**`<AdminBaseURL>/dsh`**（默认 `/app/Harness/dsh`），由控制台自己的 `AdminMux.dshUI`
反代到 dsh：

```text
浏览器 https://<fnip>:<port>/app/Harness/dsh/…
      ↓ 平台网关（gatewayPrefix=/app/Harness，socket app.sock）
Harness admin socket（AdminMux）
      ↓ 剥掉 /app/Harness/dsh，补上 dsh 会话 Cookie
dsh web @ 127.0.0.1:13080   ← 只认 /、/api、/plugins
```

**为什么必须落在控制台入口里**：飞牛 App 的网关鉴权是**按入口（gatewayPrefix）
注入**的，控制台入口的凭据不会自动带到另一个 dsh 入口。早先 dsh 用独立的
`/app/Harness/dsh` 入口（另一个 `gatewayPrefix` + `dsh.sock`），跨入口的第一请求
会被 `trim_http_cgi` 以 `200 + 13 字节 "invalid token"` 拒绝（手动刷新一次才好）；
现在控制台与 dsh 的 fpk entry 共用 `/app/Harness` 这一个前缀，请求始终在控制台
入口的鉴权范围内，不再有跨入口问题。旧的 console 内 `/<base>/gui` 路径只保留
一个 301 到 `/dsh`（非 GET/HEAD 用 308 保留方法与请求体；兼容旧书签/旧缓存页）。

fpk 的 `ui/config` 里两个 entry 都指同一入口（示例）：

```json
"Harness.control": {
  "gatewayPrefix": "/app/Harness",
  "gatewaySocket": "app.sock",
  "url": "/app/Harness"
},
"Harness.dsh": {
  "gatewayPrefix": "/app/Harness",
  "gatewaySocket": "app.sock",
  "url": "/app/Harness/dsh"
}
```

已知边界：若 `ui/config` 里保留第二个可见 entry（如上面的 `Harness.dsh`），飞牛 App
会用独立的原生 WebView 打开它（从网关日志看 UA 与请求栈都与控制台入口不同），它缓存
的网关短 token 不会像默认入口那样被 portal 的 `/app/token` 续期；几小时后第一次导航
仍会被 `trim_http_cgi` 以 `200 + 13 字节 "invalid token"` 拒绝 —— 请求到不了
`app.sock`，控制台无法拦截或重试。清 App 缓存 / 重新登录能让 App 重新取 token，但过
一段时间仍会复现。dsh 经控制台的「标签页进入」打开时（同源 iframe）复用控制台入口的
token，不走这条路径。

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
- 裸挂载点（`/app/Harness/dsh` 无尾斜杠）301 到 `/app/Harness/dsh/`（非 GET/HEAD
  用 308 保留方法与请求体）：dsh 前端的 `<base href="./">` 以**目录**为基准，缺尾
  斜杠会让相对路径解析到站点根；
- 不属于该挂载的路径直接 404（网关配置错误时不会把流量悄悄转给 dsh）。

部署时注意：

- **dsh 需 ≥ 0.1.7-alpha.1**（控制台现在只允许安装/切换 **0.2.0-rc.1 及更新版本**，见
  「dsh 服务的多版本管理」）。更早版本注入的是 `<base href="/">` 且 API/SSE/WS 用绝对根路径，
  在子路径下必然 404（正是该版本的修复项）。
- 网关只需把 `/app/Harness/…` 原样透传到 `app.sock`；前缀剥离与自留路径换算都在
  控制台内部完成，不再需要「按更长前缀优先匹配」来避免两条规则抢路径。
- 控制台 admin socket 上的 `/dsh` 挂载是 `trusted` 的（不再要求控制台登录）：它与
  网关入口同一条 socket，鉴权由 `trim_http_cgi` 完成；TCP 端口线不认网关注入的
  身份头，仍按控制台登录鉴权。
- 网关到 `app.sock` 的转发必须透传 `Upgrade` / `Connection`（与原先 dsh.sock 那条
  规则相同）：dsh 的流 mux 与终端走 WebSocket，只做普通 HTTP 代理时页面能开、实时流
  会断。

## DeepSeek 官方账号登录（控制台侧完成授权）

dsh 自带的账号登录在本部署下**用不了**，原因是**平台侧的硬规则**：`auth_init` 只接受
`http://` + 主机名 ∈ {`localhost`, `127.0.0.1`, `[::1]`} 的 `redirect_uri`（路径任意）；
公网 https/http、局域网 IP、甚至 `https://127.0.0.1:…` 一律 `50005 PAYLOAD_SCHEMA_MISMATCH`；
`login_source` 取 `web`/`desktop`/空都一样；平台只有 `auth_init`/`auth_exchange`/`auth_cancel`
三个端点，**没有设备码轮询**。而浏览器经平台网关访问时 `window.location.origin` 是公网地址 ——
dsh 的 `loginOrigin()` 与平台一样只认 loopback，于是「点登录不跳转」。

控制台因此**自己完成这套 OAuth**（`backend/account.go` + 概览页独立的「DeepSeek 账号」卡片与「添加账号」弹窗）：

| 步骤 | 谁做 |
|---|---|
| ① `POST /auth-api/v0/dsh/auth_init`（控制台自持 PKCE verifier/challenge 与 state） | 控制台 |
| ② 打开平台授权页并完成授权 | 浏览器（用户点一下） |
| ③ 回收 `?code=&state=`：loopback 来源会自动落到 `/oauth/callback`；否则把地址栏那条地址粘回控制台 | 控制台（admin 线与反代线共用同一个 handler） |
| ④ `POST auth_exchange` 换 token（校 state、单次有效、10 分钟过期） | 控制台 |
| ⑤ 按 dsh 的格式写入 `<DSH_HOME>/.credentials.yaml`（未设置时为 `$HOME/.dsh/.credentials.yaml`）：`deepseek-account-platform/default` 的 grant + `device` 记录 | 控制台 |
| ⑥ 用 dsh 自己的 `account/getState` / `getProfile` / `getBalance` 核验并展示状态 | 控制台 → dsh |

要点：

- **写入/删除都要等 dsh 认账**：dsh 的 credentials-local 用 chokidar 监听该文件（去抖
  ~100ms），写完（或退出登录删完）**立刻**查询会拿到旧状态 —— 登录会被误判成失败并回滚，
  退出登录会让界面停在「已登录」只能手动刷新。两处都在窗口内（5s / 250ms）轮询
  `account/getState` 直到 dsh 给出新状态；问不到 dsh（未就绪）不算失败，只记日志。
- **写入立刻生效，不需要重启 dsh**：`dsh-credentials-local` 用 chokidar 监听该文件，且模型侧
  每次请求现读凭据（`dsh-llm-deepseek-account` 的 `resolveToken` → `readRecord`）。真机验证：
  写入后 `account/getState` 立刻从 `signed-out` 变成 `credential-stored`，`getProfile`/`getBalance`
  随即可用。
- 写入是**行级手术**：只在 `records:` 下删/插那两条记录（flow 风格一行），文件里用户自己的
  其它 `refs`/`records` 逐字节不变；临时文件 + rename 落盘、权限强制 0600（dsh 会拒绝属主之外
  可读的凭据文档）。写完立即向 dsh 核验，**只有 dsh 明确回答 `signed-out` 才回滚并报错**；
  问不到 dsh（未就绪）不算失败。
- **回调路径必须是 `/oauth/callback`**（`accountCallbackPath`）：平台只在 `auth_init` 那一步
  宽松地接受任意路径，**用户点「登录」真正授权时会拒掉别的路径**，页面只显示「授权失败」
  （实测对照：`/oauth/callback` 成功、`/app/Harness/dsh/_account/callback` 授权失败，请求头无关）。
  反代线因此自留 `/oauth/callback`，**等于遮蔽 dsh 自己的账号回调路由** —— 本部署里 dsh 自己
  那条路本来就走不通，但将来若要恢复原生流程，得先撤掉这处自留。
- 回调地址的选择（`accountRedirectURI`）：浏览器来源是 loopback（同机 / SSH 本地转发 /
  飞牛客户端把网页代理到 `127.0.0.1:<port>`）时用 `<origin><控制台 baseurl>/oauth/callback`；
  否则用 `http://127.0.0.1:<proxyPort>/oauth/callback` 作占位 —— 平台接受、授权页能拉起；
  有到该端口的本地转发时回调自动落到控制台（`ssh -L <proxyPort>:127.0.0.1:<proxyPort>`），
  没有时用户把地址栏那条地址粘回来即可（或把 host 换成设备后打开，三种写法都自留）。
### 多账号（添加 / 切换 / 移除）

dsh **自己没有多账号概念**：账号凭据只有一条固定键（`deepseek-account-platform/default`），
谁在里面谁就是「当前账号」。所以控制台的做法是：自己存一份**账号库**，切换时把对应那份 token
重新写进那条记录。

- 账号库在**控制台自己的数据目录**：`$HARNESS_DATA_DIR/dsh-accounts.json`（0600）。
  刻意不往 dsh 的凭据文件里塞额外记录 —— 那个文件里装着用户其它密钥、格式也属于 dsh，
  控制台只写它认识的那两条（`default` / `device`）。
- **「当前生效的账号」是推导出来的**：拿凭据文件里的 token 与库里各条目比对（token 每次登录
  都会换新，条目按平台 `user.id` 归并）。库里没有、而 dsh 正在用的 token，会在查询状态时
  **自动导入**（老设备先登录后升级控制台的情况，否则列表里看不到在用的账号、切成别的就再也
  切不回来）。
- **切换** = 把该账号的 token 写进凭据记录 + 等 dsh 认账（同登录的写入路径，失败回滚到切换前
  的凭据）。凭据变更会让账号插件清掉资料/余额缓存（它订阅 `credentials/record-updated`），
  所以切完立刻就是新账号的余额与联系方式。
- **退出登录**只是让 dsh 不再使用当前凭据，**账号仍留在库里**（可以随时切回，不用重新授权）；
  **移除**才会请平台作废 token 并从库里删除。移除时若账号库写盘失败会**如实报错**（本机凭据
  已经删掉，但账号还在库里，可以重试），不会假报「已移除」。
- **账号库的每次变更都在锁内重读再落盘**（`accountVaultMutate`）：切换要等 dsh 认账、移除要请
  平台作废 token，中间隔着最长几秒；拿那一刻的库快照整份写回去，会把这期间别人的改动（新加的
  账号、刚移除的账号）静默覆盖掉。判断与写入因此在同一把锁内完成（与快捷指令的 `quickCmdsMu` 同一取舍）。
- 兼容性：`0.2.0-rc.2` 与 `0.2.1-alpha.4` 的账号插件用的是**同一个键名与同样的 grant 格式**
  （`credentialKey("deepseek-account-platform", "default")` / `"device"`），所以凭据写入对两者
  都成立；更早、没有该插件的版本只会把它当普通记录存着（`credentials-local` 是通用键值存储，
  支持 `grant` / `api-key` 两种 kind），不影响启动。

- 凭据写在 `$DSH_HOME/.credentials.yaml`（dsh 自己解析的数据根，默认 `~/.dsh`）：位置与 dsh 一致
  （见上表 `DSH_HOME`），写入后向 dsh 核验，只有它明确说「没这笔凭据」才回滚。余额按 dsh 自己的
  规则显示（截断两位 + 千分位 + 货币符号 + 子分显示 `<$0.01`，见 `frontend/src/utils/money.ts`）。
- **dsh 自己的账号入口仍然不可用**（平台规则使然）：请用概览页独立的「DeepSeek 账号」卡片
  （未登录是灰色圆形头像 + 「未登录」；登录后显示平台头像、联系方式与余额/赠送金，
  下方是账号列表：当前账号标「使用中」，其余可一键切换或移除）。点「添加账号」打开的弹窗里
  是两步流程：「打开授权链接」（跳转 / 复制）→「粘贴回调链接」并完成登录。
- 授权页是**一次性**的：同一个 `authorize_id` 授权成功后浏览器会跳到那条打不开的 `127.0.0.1`
  地址上（**跳到那里就已经表示授权成功了**，把地址粘回控制台即可完成）；此时按「后退」再点一次
  「登录」必然「授权失败」——那不是配置问题。
- 退出登录：**只删本地凭据记录**（让 dsh 不再使用这个账号），账号留在库里可随时切回，
  不碰平台侧；只有「移除」才请平台作废 token（`POST /auth-api/v0/users/logout` +
  `x-dsh-auth-token`），平台侧失败不阻塞本机删除。

### 概览页的「飞牛入口」

概览页顶栏右侧的**「标签页进入」**按钮打开**飞牛入口**——当前访问环境下经平台网关
访问 dsh 服务的地址，无需用户配置：控制台的当前访问地址（由请求的 `Origin` / `Referer`
推断，缺失时退回 `Host` + `X-Forwarded-Proto`）**剥离控制台 baseurl**
（`HARNESS_ADMIN_BASEURL`）得到飞牛 OS 的访问源，再拼上**控制台入口下的 dsh
反代路径 `/dsh/`**：

```text
控制台 http://192.168.1.111:5666/app/Harness
  ├─ 剥离 /app/Harness      → 飞牛 OS 访问源 http://192.168.1.111:5666
  └─ 拼接 /app/Harness/dsh/ → http://192.168.1.111:5666/app/Harness/dsh/
```

路径里的 `/dsh` 是**控制台入口下的子路径**，不是另一个网关入口：网关只看到
`gatewayPrefix=/app/Harness`（socket `app.sock`），控制台与 dsh 的 fpk entry 共用
这一个前缀。飞牛 App 的网关鉴权是**按入口（gatewayPrefix）注入**的，早先 dsh 用
独立入口时，跨入口的第一请求会被 `trim_http_cgi` 以 `200 + 13 字节 "invalid token"`
拒绝（手动刷新一次才好）——把 dsh 收进控制台入口（`AdminMux.dshUI`，见
`backend/admin.go`）之后这条问题从结构上消失了；dsh 前端仍然靠
`<base href="./">` 在 `/dsh/` 下解析相对路径。

换算在 `backend/admin.go` 的 `fnosEntryURL` 完成，随 `GET /api/settings` 的
`runtime.fnosEntryURL` 下发；后端拿不到访问地址时前端退回浏览器自身地址
（`document.baseURI` 的 origin + `runtime.consoleDshURL`）自行换算；两者都拿不到时
按钮不显示，避免给出指向站点根的错地址。桌面浏览器点击后新标签页打开；飞牛 App 内则用
全屏 iframe 承载（网关鉴权按入口注入，新 WebView 拿不到控制台入口的凭据）。App 内打开时
会压入一条同 URL 的历史记录：返回手势/浏览器后退先关掉这层全屏容器回到控制台，而不是
把用户直接带出应用。

这条历史记录之所以有效，是因为 portal 的返回键（`/appview` 的
`createAppViewCloseHandler`）在 `history.length > 1` 时调用 `history.back()`，而同源
iframe 里的 same-document 条目属于 joint session history —— 顶层回退会先弹掉它，并把
popstate 派发到控制台的 window。注意这个长度是**组件挂载时**读取并闭包捕获的：若 App 把
`/appview` 当作首个历史条目打开（length=1），返回键会直接 `navigate('/index')`，任何前端
代码都拦不住 —— 那种情况需要页面内的关闭入口，不能只依赖宿主返回。

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

- **跳过登录鉴权** —— 网关那条线（控制台 admin socket 上的 `/dsh` 挂载）上的请求
  只要带齐 `X-Trim-Username` + `X-Trim-Userid`，就不再被重定向到 `/_login`；
  WebSocket 升级共用同一份判定（不再回 `401`）。
- **等待界面照旧** —— 跳过鉴权不等于跳过就绪门禁：dsh 未就绪时照样是等待页，
  `/_ready` 轮询、dsh 会话凭据判定（`SessionSettled`）与转发行为都不变。
- **登录列表照旧记录** —— 网关请求不签 harness 会话 Cookie，因此按**飞牛用户
  （UID）+ 客户端 IP**标识：同一个人从不同环境（网络）访问飞牛时 IP 不同，各占一条
  记录，不会被合并刷新成同一条；同一环境的多次访问刷新同一条。列表里标注来源
  「网关访问」（端口访问一侧在 IP 前标注「端口访问」，两类来源一眼可分）、显示飞牛
  用户名与来源 IP（`X-Forwarded-For` / `X-Real-Ip`，都取不到时为空），不显示登录
  有效期，**不提供注销**（没有凭据可吊销）—— 但登录列表里的这条记录可以**手动删除**
  （`DELETE /api/visitors` 对网关条目只清列表、不拦访问；该用户再次经网关访问时
  会重新出现）。这类条目**闲置**达到登录有效期（复用 `authTTLHours`，未配置时回退
  4 小时，见 `gatewayVisitorIdleTTL`）后也会自动清除，避免列表长期堆积早已离开的
  用户。

**只有网关那条线认这三个头**（`reverseProxy.gatewayLine`，由 `AdminMux.dshUI`
构造、即控制台 admin socket 上的 `/dsh` 挂载）。TCP 端口线（`proxyPort`，局域网可达）与回环地址
一概不认：请求头是客户端自己就能加的，端口线若认它，任何人加三个头即可绕过端口
鉴权。因此**局域网直连端口的伪造请求仍然要登录**；网关线上不带身份头、或身份头
不完整时也退回原有 Cookie 鉴权（例如本机进程直连 socket）。

设置页「启用登录鉴权」开关下方有对应提示：网关访问跳过鉴权，端口访问建议开启鉴权。

---

## 反代对 dsh 产物的改写（已全部移除）

反代**不再改写任何 JS 产物**：HTML 里注入的只剩与引擎无关的引导脚本（open-in-app 屏蔽）。
历史上那层「按引擎给 dsh 打补丁」的能力已随上游自修全部拆除，别再把它加回来：

1. **改写 bundle 里的 V8-only 原生函数格式判断**。旧版 dsh 客户端用精确字符串比较判断
   「是不是本 realm 的原生构造器」，非 V8 引擎（Firefox / WebKit）上该比较恒为 `false`，
   会把普通对象误判成「非本 realm 原生原型」，表现为「载入历史…」一直转圈。上游现已改成
   引擎无关的比较（两端都取自当前引擎），原锚点在 0.2.x 的客户端产物里零命中，改写已删除。
2. **会话自愈探针**（旧 `sessionWatchdogInit`）—— 针对旧版 `ClientSessions.followCurrent()`
   没有重试路径的补偿。新版 dsh 已删除 `current` 字段与 `followCurrent()`，改由视图层
   `sessions.retain(target, { source: "mainView" })` 打开事件窗口，探针的入口条件不再成立。
3. **触屏模型菜单的 `focusout` 守卫** —— 曾随设置页的「浏览器兼容模式」（`browserCompat`）
   开关武装。上游在 `ModelSelect` 根节点用 `onMouseDown` + `preventDefault()` 保住了焦点
   （WebKit 不再在 `click` 前搬走焦点，也就没有 `relatedTarget` 为 null 的 `focusout`），
   守卫的目标消失；该修法在 `0.2.0-rc.1`（控制台的 `dshMinVersion`）及之后所有版本的产物里
   都在，注入与开关已整体移除 —— 细节见下一节。

**今后若又在非 V8 浏览器上看到「载入历史…」，不要再指望反代这层改写**，需要从 dsh
客户端的报错与上游产物定位（不是网关 / 代理）。来龙去脉见 `backend/proxy.go` 中
「转发路径上不再改写 JS 产物」那段说明。

---

## 移动端模型 / 推理等级菜单（iOS，0.2.0-rc.1 起已随上游修复）

> **历史记录**：这个缺陷已由 dsh 上游根修，控制台的兼容注入与「浏览器兼容模式」开关已整体
> 移除（见上一节第 3 条）。本节保留成因与真机排查方法，供将来复现时参考。

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
（当时用真机打点实测：菜单里已聚焦的选项 `focusout` 事件 `relatedTarget === null`；该打点
已随缺陷修复删除）。守卫于是越过
`return` 直接 `close()`，**菜单在 `mousedown` 之后、`click` 之前被卸载** → 选项的 `click`
没有目标、React 的 `onClick` 不跑，也就永远不会发出 `session/selectModel`。

真机数据（当时的诊断打点，模型座位内事件）可直接对上：

| 观察 | 数据 |
| --- | --- |
| 二级面板选项上的事件 | `pointerdown → touchstart → pointerup → mousedown`（齐全）→ **没有 `click`** |
| 紧跟着的 focusout | `target = BUTTON._7KE1Ra_option _7KE1Ra_selected, relatedTarget = null` |
| 同一时刻 | `[role=menu]` 消失（菜单被卸载）；座位本身 `disabled=false`、座位中心命中就是它、`defaultPrevented=false` |
| 根面板两行 | `click` 正常派发（那两行是进入二级面板的唯一入口，所以"能钻进去但点不动"） |

复现与回归（无需真机）：在 Chromium/WebKit 里把「菜单内 focus 搬家」按 iOS 的方式模拟
（在菜单内 `mousedown` 时对手上已聚焦的菜单按钮调用 `blur()` —— 程序化 blur 的
`relatedTarget` 同样是 null），未修复时选项点击 0 次选择、修复后 1 次。

### 修复方式（上游根修；控制台不再注入）

dsh 上游在 `ModelSelect` 的根节点上补了「保住焦点」这一步（`src/client/ModelSelect.tsx`，
`0.2.0-rc.1` 起的所有版本都在）：

```tsx
onMouseDown={(event) => {
  // WebKit blurs a focused row before click unless the button's mousedown keeps focus.
  if (event.target instanceof Element && event.target.closest('button') !== null) event.preventDefault()
}}
```

`preventDefault()` 让 WebKit 不在 `mousedown` 时搬走焦点 → 菜单内按钮之间不再产生
`relatedTarget` 为 null 的 `focusout` → `onBlur` 守卫不会被触发，菜单留到 `click` 落地。

因此控制台那段「触屏上把模型座位菜单内部的 `focusout` 拦在捕获阶段」的注入**已整体移除**，
连同它的开关（设置页「浏览器兼容模式」/ `AppConfig.BrowserCompat`）与调试标记
`window.__DSH_MODEL_MENU_FOCUS_GUARD__`。真机实测：关掉开关后 iPhone 上模型 / 推理等级菜单
一切正常。将来 dsh 若又改回「按焦点关菜单」，按下面的方法临时取现场（真机打点已随本缺陷
删除），不要默认把这层注入加回来。

### 真机诊断（打点已移除）

这类只在真机上出现的问题（事件被吞、元素在两次事件之间被卸载、focus 语义与桌面不同）靠「看
有没有发出请求」是查不出来的 —— 当时为此在反代里内建过一套默认关闭的诊断打点
（`HARNESS_DSH_DIAG` / `?dsh-diag=1`，把模型座位与菜单的现场以 URL path 形式打回
`/usr/trim/nginx/logs/access.log`）。**它只为这个缺陷而建，已随缺陷修复一并删除**；将来若又
遇到真机独有的「点了没反应」，按需临时加回同样的手法（URL path 打点 + access.log 还原），不要
常驻在注入脚本里。

### 与移动端页面插件的关系（当时的排查记录）

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

## 插件市场（dshmarket）与增强插件（dsh-fnos-advanced）

> 两者是**同一种形态**的受管插件（普通 profile 插件、npm 包），检测/安装/更新/卸载
> 共用 `backend/market.go` 的 `managedPlugin` 实现；下文以插件市场为例，增强插件
> 只是把包名换成 `dsh-fnos-advanced`、状态槽位换成 `kind=advanced`。

## 插件市场（dshmarket）的安装与更新

**形态变了**：过去 dshmarket 是被 CI 写进 dsh server 包 `dependencies` 的「内置 bundle」
（那套 `server-build.yaml` 已删除），控制台做的是「就地把 server 目录里那份
dshmarket 换成 npm 最新版」。现在 dsh 由控制台从镜像源 `npm install` 官方包，
**里面不含任何预置插件市场**，于是市场退化成一个再普通不过的 profile 插件，四个动作
全部经 dsh 自己的插件命令完成（`backend/market.go`）：

| 动作 | 命令 |
| --- | --- |
| 检测 | `dsh plugin --profile web list`（输出里有没有 `dshmarket`） |
| 安装 | `dsh plugin --profile web add dshmarket@<最新版本号> --registry=<镜像源>` |
| 更新 | 同安装（再 `add` 一次最新版本号） |
| 卸载 | `dsh plugin --profile web remove dshmarket`（**不带 `--registry`**：pnpm 的 `remove` 没有这个选项，带上会直接 `Unknown option: 'registry'` 失败；卸载也不下载任何东西，与镜像源无关，因此也不按镜像源回退） |

- **安装/更新**的镜像源与 dsh 版本安装完全一致：**只用设置页选中的那一个源**（默认阿里云，
  可选腾讯云 / 华为云 / 官方源），不自动换源，失败就报错并提示去设置页换源。**卸载不涉及 registry**：
  `remove` 走「执行一次」的通路（`runPluginCmdOnceFor`），失败就把 dsh/pnpm 的原话
  报出来，不会甩锅给镜像源。
- **未安装是正常状态**：检测**确实查到了**、只是没装时，概览页版本行显示「未安装」，
  点开弹窗即可**安装最新版本** —— 「未安装 → 装上最新版」这条路径只有控制台能提供
  （市场面板的自更新入口只适用于已经装好的情况）。
- **「查不到」不等于「未安装」**（`UpdateStatus.Unavailable`）：市场版本只能靠
  `dsh plugin --profile web list` 查，而这条命令在 dsh 服务就绪之前（启动中 / 已停止 /
  本机没选中任何 dsh 版本）必然失败。这时**不能**把空的 `localVersion` 当成「没装」，
  否则用户会以为市场没装、去点安装。规则：
  - 检测失败 → `unavailable=true`，版本行显示 **「—」**，市场弹窗只说「暂时查不到」并
    **不给安装入口**（装没装过都不知道，谈不上装哪个），诊断文本（`marketDir`）仍照给；
  - 只有命令成功退出才写 `localVersion`：空 = 真没装 →「未安装」；
  - 检测失败时**保留上一次已知的版本号**（插件不会因为 dsh 重启而消失，清空比留着旧值
    更像谎话），只有从未查到过才没有值；
  - 首帧（进程刚起、还没检测过）也是 `unavailable=true`：没检测过 = 不知道。
  实现见 `market.go` 的 `applyPluginLocalResult`（本地检测结果的**唯一**写入口，市场与增强插件共用）与
  回归测试 `backend/market_unavailable_test.go`。
- **dsh 一就绪就重查**（`RefreshMarketAfterDshStart`）：既然「查不到」是常态，就必须在
  能查的时刻补一次，否则「—」会一直挂到下一次小时级检测。触发点：控制台启动流水线收尾
  （dsh 起来 + 依赖装好）、概览页的「启动 / 重启 dsh」、切换 dsh 版本。异步执行、只查一次
  （探测失败的结果**不进缓存**，所以下一次触发一定真查）。
- **缓存**：本机装的是哪一版（`dsh plugin list` 子进程）与镜像源上的最新版都会缓存，
  只在安装/卸载完成后、后台自动检测、手动「检查更新」以及「dsh 刚就绪」时刷新
  （见下一条）—— 详见「dsh 服务的多版本管理」末尾的缓存策略表。本地检测**失败**
  （`unavailable`）的结果不缓存：它只是「这次没查到」，缓存住会让「—」一直挂着。
- **「最新版本」= 版本号最高的那一版，不是 `dist-tags.latest`**：`latest` 是发布者手动
  移动的标签，可能滞后（发了新版忘了挪标签）、可能指向另一条线（稳定线 / 预览线），
  镜像源同步时也可能只更新了版本集合而没更新标签。因此检测与安装都按版本号排序取最高
  （`newestVersion`），安装命令带**精确版本号**（`add dshmarket@<该版本>`，不是 `@latest`）。
  副产物：某个镜像源还没同步到这一版时，那次尝试会失败并提示去设置页换源 ——
  控制台**不会**自动换下一个源。
  （dsh 服务版本行也是同一套语义：红点只看**版本号大小** —— 版本列表里最高的一版比当前
  选中的高就亮，不区分 `dist-tags`；列表里仍标注 latest / alpha / next，但标签不参与判定，
  见 `server.go` 的 `refreshDshStatus`。）
- **没有备份 / 回滚**：安装失败就是把错误推给弹窗，不生成备份包，也没有回滚入口。
- **不自己下载 tarball、也不做完整性校验**：pnpm 会按 registry 元数据里的 `integrity`
  校验它下载的字节，控制台再下一份只是重复劳动。
- **顺序：先跑插件命令、再重启 dsh**。安装/卸载都是 pnpm 在 profile 目录里跑，
  会持有 profile 写锁（`profiles/web/package.json.lock`），**绝不能在它跑的时候停 dsh**
  ——停 dsh 会连带终止持有锁的那个进程组，留下陈旧锁，之后所有插件操作都会白等 120 秒
  再失败。因此命令跑完才 `restartDshForPlugin()`：先过忙守卫、停 dsh、等端口释放、
  `startDshCaptured()` 拉起并重新换取会话凭据，再等它真正监听端口（上限 30 秒；进程退出
  立即判失败）让新 bundle 生效。
- **进度**：阶段（安装中 / 卸载中）+ 插件命令的输出尾行 + 「正在重启 dsh 服务」，
  经同一条 SSE 推送。

**忙守卫**（`replaceBusyGuard`）是所有这些停 dsh 路径的共同前置检查：正在装/卸插件时
拒绝操作并说明原因（被拒绝时**不产生任何停机、也不改盘**）。两个来源互补：

- 市场面板发起的安装 → 查 `/dsh-market/status` 的 `busy`；
- 控制台自己的插件命令（插件页、市场弹窗）→ 查 `DshManager.PluginCmdRunning()`。

市场不回答（没装市场 / dsh 已经坏了）时视为「不忙」，所以恢复 dsh 数据这类抢修操作
不会被挡住。**例外**是概览页的「停止 / 重启 dsh 服务」与插件页的「重启生效」按钮 ——
那是用户明确的即时意图，弹窗打开时查一次 `GET /api/dsh/busy` 并显示风险提示，由用户决定。

## 备份包的留存规则

`<数据目录>/backup/`（默认 `$TRIM_PKGVAR/backup`）下按 `<类型>-<版本>-<时间戳>.tar.gz` 命名。
现在**唯一需要长期留存的只有用户主动的 dsh 数据备份**：

| 类型 | 谁生成 | 留存 | 原因 |
|---|---|---|---|
| `dsh-data-*` | 目录页「备份」（用户主动） | **保留** | 用户的数据备份，只能手动删；不参与自动清理 |
| `harness-*` / `server-*` / `market-*` | 旧版本的自我更新 / 服务更新 / 市场更新 | **不再生成** | 三条链路都已整体移除（升级只走 fpk，dsh 换版本 = 换一个已安装的版本目录） |

老版本留下的 `server-*` / `market-*` 包仍由每日清理任务按 30 天回收。

**新增更新分支时沿用同一条规则**：没有回滚入口就别把备份包留在盘上。

### dsh 数据备份：服务端异步执行（进度 / 取消）

目录页的「备份」**在服务端 goroutine 里打包**，弹窗只是观察者 —— 这条边界的三个后果
都必须保持：

| 接口 | 行为 |
|---|---|
| `POST /api/dsh/backup` | 只做「主目录 / `.dsh` 存在性 + 互斥」检查后立刻返回，打包在后台进行 |
| `GET /api/dsh/backup/status` | 进度快照：`bytes`/`totalBytes`（预扫描的原始总字节数）、`files`/`totalFiles`、`running`/`cancelling`/`done`/`ok`/`cancelled` |
| `POST /api/dsh/backup/cancel` | 中止打包并**删除不完整的备份文件**；没有备份在跑时是幂等空操作 |

- **关掉弹窗（乃至刷新页面、切换视图）都不终止备份**：进度状态是进程级的
  （`update.go` 的 `dshBackup` 跟踪器），前端 `composables/useDshBackup.ts` 是模块级共享
  状态 —— 重开弹窗时先拉一次快照再续上轮询，落定即停，不留长期计时器。
- 取消是**取消**而不是失败：走 `errBackupCancelled` 哨兵错误，只记 INFO、不记 ERROR，
  也不把半成品留在 `backup/` 里（半个包比没有更糟）。
- 备份要读整个 `~/.dsh`，因此与「恢复数据 / 受管插件的安装卸载」共用一把 `applying`
  互斥（双向）：备份进行中恢复被拒，插件操作进行中备份被拒。
- 打包**只有一份实现**：`tgzDirAsProgress`（`tgzDirAs` 是它的 `prog=nil` 包装）。
  进度按块汇报、每块检查一次取消 —— 大文件也能被及时取消。

---

## dsh 服务的多版本管理

dsh 服务**不再是内置在 fpk 安装包里的预构建产物**，也不再从 GitHub Release 下载压缩包。
控制台按需从设置页选中的 **npm 镜像源**安装官方 npm 包（`backend/server.go`）：

```
${TRIM_PKGVAR}/server/<版本>/node_modules/.bin/dsh
```

| 环节 | 做法 |
| --- | --- |
| 版本列表 | 镜像源的 packument（`/@deepseek-ai%2fdsh`），**隐藏 `0.2.0-rc.1` 之前的所有版本**（新版控制台锚定 0.2.0-rc.1 及更新版本；更早的 dsh 前端也不走文档相对路径，在控制台的子路径挂载下必然 404），并标注 `dist-tags`（latest / alpha / next…）。**「有更新」的红点/「最新版本」取列表里版本号最高的一版**，标签只作展示、不参与判定（`refreshDshStatus`）。**列表缓存到下一次刷新为止**：打开弹窗、切页面、状态轮询一律吃缓存（不联网），只有弹窗里的「刷新」、后台自动检测 / 手动「检查更新」以及设置页保存新镜像源后（会跑一次完整检测）会重拉（见下） |
| 镜像源 | **设置页选中的那一个源**（默认阿里云）：阿里云 `registry.npmmirror.com` / 腾讯云 `mirrors.cloud.tencent.com/npm` / 华为云 `repo.huaweicloud.com/repository/npm` / 官方源 `registry.npmjs.org`；**只用所选源，不自动换源** |
| 失败处理 | `npm install --fetch-timeout=5000 --fetch-retries=0`：任意一次 HTTP 请求 5 秒拿不到响应即失败；同一个源**不重试**，直接报错并提示去设置页切换镜像源 |
| 安装命令 | `npm install --prefix <版本目录> @deepseek-ai/dsh@<版本> --save-exact --registry=<镜像源>`，用 `PATH` 中 node24 自带的 npm；缓存在 `<数据目录>/npm-cache`（不写用户的 HOME） |
| 进度 | 逐行解析 `--loglevel=http` 的包获取行：阶段（下载 / 处理依赖包 / 校验）+「已获取 N 个包」+ 输出尾行。依赖总量 npm 不预先给出，因此**不显示假百分比**（不确定进度条） |
| 取消 | 支持取消（**不支持暂停**）：终止整个 npm 进程组、**删除安装目录与 npm 下载缓存**，状态标记为「已取消」 |
| 安装后 | 打上 fnOS 需要的**附件 fsync 补丁**（见下），再执行 `<版本>/node_modules/.bin/dsh -V` 校验版本号；校验不过则删掉该目录并报错（不自动换源） |
| 切换 | 概览页版本列表里点「切换」→ 写 `config.json` 的 `dshVersion` → 停 dsh → 用该版本启动（`startDshCaptured` 重新换取会话凭据）。**切换前先过忙守卫**（正在装插件时拒绝） |
| 删除 | 删除版本目录（磁盘占用约 500 MB / 版本）。**当前正在使用的版本不允许删除** |
| 未安装 | 没有选中任何可用版本时：概览页版本行显示「未安装」，反代等待页显示「尚未安装 dsh 服务」并提示去概览页下载 + 切换（`boot.go` 的 `phaseNotInstalled`） |

**停止维护的旧版本**（低于 `0.2.0-rc.1`）—— 列表里不展示、也不能下载或切换。但如果它
**已经装在盘上**，会被标上「停止维护」单独补到列表**最底部**（`snapshot()` 里带 `Deprecated`
的条目），行内只留两个动作：「删除」，以及它**正被使用时**的「切换到 <最新版>」（且最新版本地
已装着才出现）。**不允许切回**：直接调接口 `Switch` 也会被拒（`err_version_deprecated`）。
这样既不留下「看不见又删不掉」的 500 MB 目录，也不会把服务切回一个没人维护的版本。

**版本数据的缓存策略**（`refreshVersions(force)` / `detectPluginLocal(force)`）：这两个来源
都是「慢查询」——前者要打一次所选镜像源，后者要起一个 `dsh plugin --profile web list` 子进程
（1~3 秒）——因此都按**缓存优先**处理，`force=false` 时只要有缓存就直接返回、不联网：

| 触发 | dsh 版本列表 | 市场本地检测 / 最新版 |
| --- | --- | --- |
| 打开弹窗 / 切页面 / 状态轮询 / 诊断接口 | 只读缓存（不联网；为空就显示空列表） | 吃缓存（缓存为空时才查一次） |
| 弹窗里的「刷新」按钮 | **强制重拉** | —（市场弹窗没有刷新按钮，改由下面的检测刷新） |
| 后台自动检测（每小时）/ 手动「检查更新」 | **强制重拉** | **强制重查**（本地版本 + 镜像源最新版） |
| 设置页保存新的 npm 镜像源 | **强制重拉**（异步） | **强制重查**（本地版本 + 新源最新版） |
| 安装/卸载市场之后 | — | **强制重查**（本机版本确实变了） |

失败时**保留上一份缓存**（旧列表/旧版本号仍然可用），只把错误写进状态与弹窗提示，
所以「刷新失败」不会把界面变空。

`<版本>/node_modules/.bin` 会前置到 dsh 进程与 `dsh plugin …` 子进程的 PATH
（`DshManager.buildEnv`），而启动 dsh / 执行 CLI 用的是**该版本的绝对路径**
（`DshManager.dshBinPath`）—— `exec.Command("dsh")` 的 LookPath 走的是 harness 自己的
PATH，不能用来解析「当前选中的版本」。

**Web 终端里的 `dsh` 走稳定入口** `${数据目录}/server/.bin/dsh`：它是符号链接，启动时
由 `syncDshShim` 对齐到当前选中版本，切换版本时原子改指向。终端 PATH 只在会话创建时
构造一次，直接沿用启动环境的旧 `<版本>/node_modules/.bin` 会在切换/删除版本后让 `dsh`
命令消失；稳定入口让路径恒定存在，已经开着的终端会话也能跟着切换（`.bin` 以点开头，
不会被版本列表当成一个版本）。

> **附件 fsync 补丁（必须打，否则 fnOS 上传文件 / 图片整体失败）**：
> `@deepseek-ai/dsh-attachment-local` 落盘附件时会对每级祖先目录 `open(O_RDONLY) + fsync`
> 并一路上溯到 `/`，而 fnOS 把 `/vol1` 与 `/vol1/@appshare` 设为 mode 000（仅由 trim_acl
> 授予穿越），应用读不了这些祖先目录 → `EACCES`。补丁把这段上溯包进 try/catch，遇到
> `EACCES` / `EPERM` 即停止上溯（不可读的祖先本就无法 fsync）。过去它由 CI 打在预构建包里
> （那套 workflow 已删除），现在 dsh 由控制台自己 `npm install`，因此必须在安装后由控制台
> 补上（`server.go` 的 `patchAttachmentFsync`：幂等、带锚点校验，锚点缺失时只记 WARN
> 不阻塞安装）。

---

## 控制台版本与升级方式

**控制台自身没有就地自我更新**：`backend/update.go` 里原有的「下载发布资产 → 解压 →
备份并替换二进制 → `syscall.Exec` 换映像」整条链路（连同 下载/安装/暂停/取消/删除更新包
的 API 与前端弹窗、`ProxyUpdate`「代理harness更新」开关、发布资产的 `.sha256` 校验）
已整体移除。

升级控制台**只走 fpk 安装包**：平台侧的部署脚本、依赖与 dsh 的配套版本都在 fpk 里，
换包会把它们一起换掉；让控制台自己替换一个二进制，只会得到「二进制换了、平台侧没换」
的半升级状态。

由此带来的约定：

- 概览页的版本卡片只剩 **dsh 服务 / 插件市场 / 增强插件** 三行；控制台版本不在界面上
  展示（需要时看 fpk 的 manifest 或启动日志）。
- 更新检测（每小时 + 手动「检查更新」）只针对这三条链路，全部走设置页选中的 npm 镜像源。
- `harnessVersion`（`-ldflags -X main.harnessVersion=…`）仍保留：它作为控制台版本号
  随官方账号登录的客户端元数据上报，不再参与任何更新判定。
- **不要再把自我更新的任何变体加回来**（包括「只允许小版本」那套闸门、GitHub tag 探测、
  发布资产下载、代理更新开关）：要升级就发新 fpk。

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
   （不再靠整页刷新撞时机）；无 JS 客户端仍有 10 秒 `Refresh` 响应头兜底。

放行（把请求转发给 dsh）**只有一个判定入口**：`reverseProxy.state()`，三个条件全部满足
才放行，等待页与 `/_ready` 的返回值都来自同一处，避免“轮询说就绪 → 跳转 → 又是等待页”
的抖动：

| 条件 | 含义 | 不满足时的阶段 |
| --- | --- | --- |
| 启动流水线已收尾 | 不在 `starting / auth / deps` 阶段（收尾可能重启 dsh，提前放行会让界面随即失效） | `starting` / `auth` / `deps`（页面统一显示「等待服务就绪」） |
| dsh 端口已监听 | 配置的 `dshPort` 可连（反代的上游） | 进程在 → `starting`；进程不在 → `stopped` |
| 本代凭据已落定 | `DshManager.SessionSettled()`：本代 dsh 的 `dsh-auth-*` Cookie 确实可用 | 未落定 → `auth`；确认拿不到 → `auth-failed` |

`phase` 取值（`/_ready` 原样返回，便于排查；页面只区分「等待中」与「需要动手」两类）：

- 等待中（页面统一显示「正在等待 DeepSeek Harness 服务就绪」）：`starting`（拉起 dsh）、
  `auth`（换取凭据）、`deps`（安装/校正 node-pty），以及启动流水线之外的 `starting`
  （dsh 正在自重启/被拉起）。
- 需要用户动手（页面停转并给出对应指引）：`stopped`（dsh 未在运行）、`failed`
  （`dsh.Start()` 失败，`detail` 为原因）、`disabled`（`HARNESS_AUTOSTART=0`）、
  `not-installed`（本机没有任何已选中的 dsh 版本，`detail` 给出安装目录与下一步：
  去概览页下载 + 切换）、`auth-failed`（端口已通但没拿到可用凭据，后台继续补偿）。
- 等待超过 **30 秒**才出现“可能需要排查”的黄色提示（旧版固定在 10 秒后就报
  「启动失败」，属误报）。

改动这块时注意：

- 凡是**启动或重启 dsh** 的路径，都必须经 `captureDshSession` 换取凭据（它会调用
  `markSessionSettledGen`）。漏掉就不会标记落定，反代会一直停在等待页。
  `DshManager.Start()` 每次都会递增启动代号，使上一代凭据立即失效。
- `WaitToken` 最多等 **20 秒**。等不到 token 时先探测上一代保留的会话 cookie；仍不可用
  就标记 `auth-failed` 并启动后台补偿（继续等 token，最长 5 分钟），**不会**把用户
  放行到没有凭据的 dsh（那只会看到 401）。
- WebSocket 升级同样有门禁：未登录 → `401`；凭据未落定 → `503`；端口未就绪时保留
  10 秒容忍窗口（dsh 市场一键自重启期间客户端已在界面上，等它回来比立刻断开友好）。

---

## 主要流程

1. **启动** — 解析环境变量 → 读取配置 → 校验密码 → 启动 Admin socket →
   **启动反向代理**（TCP 根挂载 + 控制台 admin socket 上的 `/dsh` 子路径挂载；先监听、
   先鉴权，dsh 未就绪时给等待页）→ 自动启动 dsh：**先看有没有选中的 dsh 版本**，没有就进入
   `not-installed` 阶段（等待页提示去概览页下载 + 切换），有则启动该版本的
   `dsh web --no-open --port <port>`（除非 `HARNESS_AUTOSTART=0`）。
2. **凭据交换** — 从 dsh 日志扫描一次性访问 token（`?token=`），用它访问一次
   dsh 地址，从 `Set-Cookie` 换取 `dsh-auth-*` 会话 Cookie，并标记本代凭据已落定
   （`markSessionSettledGen`，反代据此放行）。
3. **放行** — 三条件齐备（流水线收尾 + 端口就绪 + 凭据落定）后，反代携带该 Cookie
   把 dsh 反代到配置的反代端口；等待页轮询到 `ready` 即自动跳转。详见上文
   「启动期间的等待页与放行门禁」。
4. **node-pty** — 等待 pnpm 与 `$HOME/.dsh/profiles/web` 就绪后校正 node-pty
   （在 `pnpm-workspace.yaml` 里固定版本、清理旧 patch 产物；只有发现旧 patch 时才
   `pnpm install` 并重启 dsh。仍在放行门禁内：此阶段即使端口已通也不放行）。
5. **版本与更新** — 两条链路的形态不同（控制台自身只能换 fpk，见上文
   「控制台版本与升级方式」）：
   - **dsh 服务（多版本）**：从 npm 镜像源 `npm install --prefix <数据目录>/server/<版本>`
     官方包，可同时装多个版本并切换；下载可取消（清安装目录 + npm 缓存），安装成功可删除，
     未安装任何版本时控制台显示「未安装」、反代等待页显示「尚未安装 dsh 服务」。
     详见上文「dsh 服务的多版本管理」。**没有压缩包备份 / 回滚**：旧版本目录留着就是回退。
   - **插件市场**：`dsh plugin --profile web add dshmarket@<最新版本号> --registry=<所选源>` /
     `remove dshmarket`（不带 registry；安装带精确版本号，不用 `@latest` 标签），
     未安装时控制台可装上最新版；安装/卸载成功后重启 dsh 让新 bundle 生效。
     详见上文「插件市场（dshmarket）的安装与更新」。同样没有备份 / 回滚。
6. **退出** — 收到 `SIGINT/SIGTERM/SIGQUIT` 或 `stopCh` 后停止 dsh、移除 socket。

---

## 常见问题排查

- **Admin socket 已被占用** — 说明已有实例在运行，主进程会直接退出。
- **鉴权未启用** — 只有未设置 `password`（或关闭鉴权开关）时后端才打印「任何人可访问」的
  警告；密码强度不合规只会记一条 ERROR，该密码仍用于登录鉴权。
- **反代停在「无法获取 dsh 访问凭据」** — 首次等待窗口（20 秒）内没拿到可用 token，上一代
  会话 cookie 也探测不通；控制台会在后台继续重试（最长 5 分钟），等待页停在 `auth-failed`。
  可重启 dsh 或查看日志；不会再放行到没有凭据的 dsh。
- **控制台启动期间打开反代地址只看到等待页** — 正常：dsh 还没就绪。页面只显示
  「正在等待 DeepSeek Harness 服务就绪」，就绪后自动跳转（内部阶段不对外展示）；等待
  超过 30 秒会出现“可能需要排查”的提示。若一直停在那里，去控制台看日志与概览页的 dsh
  版本状态（等待页在 `failed` / `not-installed` 阶段会直接给出失败原因或下一步）。
  想手动确认状态可带登录 Cookie 请求 `GET /_ready`
  （返回 `{phase, ready, detail}`，`phase` 见上文「启动期间的等待页与放行门禁」）。
- **dsh 重启后页面白屏/接口报错** — dsh 正在重启（市场一键重启、切换 dsh 版本等），反代
  此时返回等待页；刷新即可，等待页也会在 dsh 回来后自动跳转。
- **概览页 dsh 服务版本显示「未安装」** — 本机 `${TRIM_PKGVAR}/server/` 下没有任何已选中
  的可用版本：去版本列表里下载一个并点「切换」。这是新形态的正常初始状态（dsh 不再随 fpk
  预置），不是故障。
- **下载 dsh 版本失败（所选 npm 镜像源不可用）** — 错误里会点明是哪个源。去设置页换一个
  源再重试（默认阿里云，可选腾讯云 / 华为云 / 官方源）。安装直连镜像源、不走那套 http 代理
  设置；官方源 `registry.npmjs.org` 在国内通常不可直连，只有主动选中时才会使用。
- **dsh 装好后在 fnOS 上无法上传文件 / 图片（EACCES）** — 附件 fsync 补丁没打上。看日志里
  有没有 `attachment fsync patch not applied`（安装完成后注入，见「dsh 服务的多版本管理」）；
  该补丁失败不会让安装失败，但会让上传功能不可用。
- **CPU/内存读不到** — dsh 装插件自重启后 PID 变化，后端会自动在 `/proc` 中重新发现。
- **Firefox / Safari 打开会话只有「载入历史…」（且一直显示「深度求索中…」）** —
  dsh 客户端旧版的原生函数格式判断只适配 V8，**上游现已改成引擎无关的比较**，控制台那层
  bundle 改写也随之移除（见「反代对 dsh 产物的改写（已全部移除）」一节）。若仍出现，先确认
  dsh 版本是否过旧（新版控制台只允许 0.2.0-rc.1 及更新版本），再按 dsh 客户端的报错定位 ——
  反代已经不再改写任何 JS 产物，别指望它。
- **改动前端（内嵌 dist）或 dsh 插件资源后刷新看不到变化** — 反代注入走 HTML，每次刷新都
  回源，普通刷新即生效；但 dsh 插件资源带一年期 `immutable` 强缓存且无 `ETag`，普通刷新不回源，
  需清除浏览器缓存或强制刷新；必要时重启 dsh 使其 `rev` 变化。
- **`vue-tsc` 报 `baseUrl` 已废弃（TS5101）** — TypeScript 6 起 `baseUrl` 被标记废弃。
  本项目已移除 `baseUrl`，`paths` 改用相对本 tsconfig 的 `./src/*` 写法（`@/*` 别名
  在 vue-tsc 与 Vite 两侧均保持可用）。改回 `baseUrl` 会重新触发该报错。
- **前端类型检查命令** — `cd frontend && npm run typecheck`（即 `vue-tsc --noEmit`）。
  注意 TypeScript 需为 **6.x**：TS 7 移除了 `typescript/lib/tsc` 子路径导出，
  `vue-tsc` 3.x 依赖它，会直接以 `ERR_PACKAGE_PATH_NOT_EXPORTED` 崩溃。

---

## 许可

[Apache-2.0](LICENSE) © 2026 shuangji66
