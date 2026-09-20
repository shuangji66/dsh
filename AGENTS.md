# AGENTS.md — 给 AI 代理与开发者的协作指引

本文档面向在本仓库内修改代码的开发者与 AI 代理（agent），记录**关键约定、易踩的坑
与必须遵守的规则**，以降低误改风险、保持一致风格。

---

## 1. 项目本质

- 本仓库是 **DeepSeek Harness 控制台**：Go 后端 + Vue 3 前端。
- 它**守护并管理** `dsh web` 进程，不是 dsh 本体。
- 部署目标为 **fnOS / TRIM 平台**（路径习惯如 `/var/apps/Harness`、`/vol1/@appshare/Harness`）。
- 最终产物是**单个自包含 Go 二进制**：前端 `dist` 经 `//go:embed` 打入 `backend/embed`。

---

## 2. 构建约定（重要）

- 前端用 **相对 base**（`base: './'`）构建，**禁止**把 baseurl 硬编码进产物。
  真实 baseurl 由后端在运行时注入 `<base href>`（见 `admin.go` 的 `rewriteIndexBase`）
  并改写 `./assets/...` 前缀。
- 前端资源路径应基于 `document.baseURI` 解析（见 `frontend/src/serverapi/index.ts`
  的 `runtimeBase()`），不要依赖构建时的相对 `BASE_URL`。
- `Makefile` 与 `build.sh` 等价；两者都先把前端 `dist` 拷入 `backend/embed`，再
  `go clean -cache` 后构建 Go 二进制。**Go 缓存默认放项目本地**（`.gocache` / `.gopath`，
  可用环境变量覆盖）。
- 每次前端改动后若需生效，必须**重新构建**（`make` / `./build.sh`）并验证实际 URL，
  仅改源码不重新构建不会让已嵌入的二进制更新。
- `build.sh` 里的 `ROOT/.../dsh/backend` 是**平台部署脚本的遗留路径习惯**，开发时
  本仓库结构是顶层 `backend/` 与 `frontend/`；保持二者行为一致即可，不要被前者误导。

---

## 3. 架构要点与模块职责

**后端（Go，`backend/`）**

- `main.go` — 入口。顺序：解析环境 → 建日志 → 写 PID → 查 socket 占用 → 读配置 →
  校验密码 → 起 Admin socket → **起反代**（早于 dsh：先监听、先鉴权，未就绪给等待页）
  → 起 dsh（非 `HARNESS_AUTOSTART=0`）→ 换 Cookie → 装 node-pty → 标记就绪 → 等信号退出。
- `boot.go` — 启动阶段状态机（`starting/auth/deps/ready/failed/disabled`）+ `proxyState`。
  反代据此决定等待页显示什么、能否放行；阶段由 `main.go` 推进。
- `config.go` — `AppConfig`（前端可改）与 `RuntimeEnv`（环境变量）。**反向代理端口
  （`ProxyPort`）只从环境变量读，不随配置保存。**
- `admin.go` — Admin mux（Unix socket）：`buildHandler()` 里一个大的 `switch` 分发
  所有 `/api/*` 路由；`spaHandler` 提供内嵌前端。
- `dsh.go` — `DshManager`：进程生命周期；`effectivePID`/`findDshPid` 处理**装插件自重启**
  后 PID 变化（扫 `/proc/<pid>/cmdline` 匹配 `dsh web ... --port <port>`）；
  `tokenScanner` 从 dsh 日志捕获 `?token=`；`ExchangeToken` 换 `dsh-auth-*` Cookie；
  `SessionSettled`/`markSessionSettled` 记录「本代凭据是否已换取完成」（反代放行门禁）。
- `proxy.go` — 反向代理，转发时**携带 dsh 会话 Cookie**；`ServeHTTP` 里顺序是
  「鉴权路由 → 就绪状态 `/_ready` → 鉴权 → 等待页/转发」，放行判定只在
  `reverseProxy.state()` 一处；等待页（`waitingPageHTML`）轮询 `/_ready` 并自动跳转。
- `terminal.go` — WebSocket + `creack/pty` 的交互式 bash。
- `update.go` — 更新 harness / dsh 服务与插件市场：版本检测、下载、备份与回滚；
  `harnessVersion` 由 `-ldflags -X` 注入。下载策略按类型分开（`downloadPlanFor`）：
  发布资产（harness/dsh）走「代理+直连各 2 次 + HTTP Range 断点续传 + 暂停/取消」，
  插件市场**只直连、不支持续传/暂停**（包小、registry 通常不需要代理）。
- `market.go` — 插件市场（dshmarket）就地更新：解析实际生效的安装位置（server 包内置
  vs profile 自带）、npm registry 取版本、完整性校验、原子替换与失败回滚。
- `install.go` — 自动安装并 patch `node-pty`（等待 `$HOME/.dsh/profiles/web` 目录）。
- `auth.go` / `visitors.go` / `sse.go` — 登录鉴权、访客跟踪（SSE 推送）、事件流。
- `quickcmds.go` — 终端快捷指令持久化（`HARNESS_QUICK_CMDS_FILE`）。
- `fnos.go` — fnOS open-gateway 客户端（`/var/run/trim_open_gateway_apiscope.socket`）。

**前端（Vue 3，`frontend/`）**

- **强制 Composition API + `<script setup>` + TypeScript**（Vue 3，SSR 下用 Volar / vue-tsc）。
- 子页面统一经 `/` 下的 `?view=xxx` 查询参数切换（`router/index.ts`），避免真实历史
  记录；旧 `/directory` 等路径做 302 重定向。
- 状态用 **Pinia**（`stores/`）；主题 / i18n / 偏好用 `composables/`。
- 终端视图被 `<KeepAlive>` 缓存，切换标签不销毁会话。
- i18n（`useI18n.ts`）只存 localStorage，不随设置持久化到后端。

---

## 4. 必须遵守的规则（Agent 优先）

1. **不要破坏“运行时 baseurl”机制** —— 改前端资源路径/API 时，始终经
   `runtimeBase()` / `document.baseURI`，不要硬编码前缀。
2. **不要硬编码平台路径** —— 用环境变量（`TRIM_APPDEST`、`TRIM_PKGVAR`、
   `HARNESS_*`）而非写死 `/var/apps/Harness`（除非是 `install.go`/`fnos.go` 等
   明确约定平台常量的位置）。
3. **反向代理端口不可被配置保存覆盖** —— 端口只来自 `PROXY_PORT` 环境变量。
4. **PID 感知** —— 涉及 dsh 进程生命周期/状态时，用 `effectivePID()` 而非直接信任
   `m.cmd.Process.Pid`（dsh 会装插件自重启）。
5. **前端改动必须重编译验证** —— 修改前端后运行构建并刷新确认，别只改文件。
6. **遵循 Vue 最佳实践** —— 优先 Composition API；状态进 Pinia；可复用逻辑进
   composables。
7. **保持双语注释习惯** —— 现有代码中文注释占多数，新增注释建议保持项目既有风格。

---

## 5. 常见的坑（Gotchas）

- **`go clean -cache` 会清掉整个 Go 构建缓存** —— 这是构建脚本的刻意行为，保证
  embed 资产改动一定被编进去；不要误以为异常。
- **`embeddedFS` 的 embed 目录在构建后会被 `rm -rf`** —— 编译产物里已内含资源，
  源码树中 `backend/embed` 通常不存在/为空，属正常。
- **token 仅捕获一次** —— `tokenScanner` 命中后回调置空；每次 `dsh.Start()` 会重置
  token 与 Cookie。旧版 dsh 不打印 token，`WaitToken` 会空等超时返回 `""`。
- **反代放行门禁只有一个入口 `reverseProxy.state()`** —— 反代从控制台启动的第一刻就在
  监听（早于 dsh），放行必须同时满足「启动流水线已收尾（`bootState` 不是
  starting/auth/deps）+ dsh 端口已监听 + `DshManager.SessionSettled()`」；等待页与
  `GET /_ready` 共用这一份判定，避免“轮询说就绪 → 跳转 → 又是等待页”的抖动。
  由此两条硬约束：
  1. **任何启动/重启 dsh 的路径都必须经 `captureDshSession`** —— `Start()` 每次递增
    启动代号使上一代凭据失效，`captureDshSession` 在等待 token 结束（拿到或 15 秒超时）
     后调用 `markSessionSettled` 标记落定。漏掉就不会落定，反代永远停在等待页。
  2. **不要为了“更快看到界面”放宽门禁**（例如只留 `checker.quick()`）：端口通了但凭据
     没换到就放行，只会把用户送进 dsh 的未授权响应；而把 `deps` 阶段的放行提前，会让
     流水线收尾重启 dsh 时界面随即失效。
- **`PROFILE_TEMPLATES.web.bundles` 注入** —— 只在 `server-build.yaml` 的 CI 中对
  `dsh-app-boot` 做，本地不涉及。
- **代理端口默认 `13079`、dsh 端口默认 `13080`** —— 冲突排查先看这两个。
- **dsh 的跨进程写锁会因「持有者被杀」而残留，代价是 120 秒白等** ——
  `@deepseek-ai/dsh-atomic-write` 的 `withFileLock` 用 `<文件>.lock` + `wx` 独占创建、
  只在 `finally` 里删除；持有者被杀死（用户取消安装、进程被重启带走）就永久残留，
  而实现上**不清理陈旧锁**，等待上限又可以是 120 秒（plugin-manager 的 `lockWaitMs`
  默认值）。于是 `profiles/web/package.json.lock` 一旦残留，「插件列表 / 安装 / 更新」
  全部会等满 120 秒再失败（现象：市场内无法更新、控制台插件列表空白，浏览器轮询还会
  不断堆积挂起的 dsh 进程）。harness 现在在**启动 dsh 前**与**执行 `dsh plugin …` 前**
  会清掉「锁文件里的 PID 已不存在」的锁（`DshManager.cleanStaleProfileLocks`）。
  由此推出三条硬约束：
  1. **不要按进程名杀 node**：`pkill -x MainThread|node-MainThread` 会命中该用户下
     *所有* Node 进程 —— 包括插件市场正在跑的 `dsh plugin --profile web add`（它正持有
     上面那把锁）及其 pnpm 子进程，杀完就留下陈旧锁。`DshManager.Stop` 已改为只按
     PID / 进程组精准终止，不要再退回按名杀。
  2. **凡是会停 dsh（或删 ~/.dsh）的操作，动手前必须过忙守卫**：插件的安装/卸载
     （不论市场面板发起还是控制台插件页发起）都在 dsh 进程内持有那把锁，停 dsh 会连带
     终止它的进程组，把持有者一起带走 —— 安装白做，还留下陈旧锁。
     - `UpdateManager.stopDshForReplacement()`：替换 `server/` 产物的三条路径
       （更新 dsh 服务 / 更新市场 / 回滚 server 备份）统一走它 —— 先 `replaceBusyGuard()`，
       再停 dsh 并等端口释放；被拒绝时不产生任何停机、不改盘。
     - `replaceBusyGuard()` 也用在**更新 harness 控制台**与**恢复 dsh 数据**（会删 `~/.dsh`）
       的入口，动手之前先挡。
     - 守卫来源：市场 `/dsh-market/status` 的 busy + `DshManager.PluginCmdRunning()`；
       市场不回答视为不忙（回滚/恢复这类抢修不被挡）。
     - 唯一例外是概览页的「停止/重启 dsh」按钮：那是用户的即时意图，**不挡**；改为在确认
       弹窗里提示 —— 弹窗打开时查 `GET /api/dsh/busy`（`AdminMux.dshBusySnapshot()`），
       有插件操作在跑就显示风险提示。这个端点刻意不塞进高频轮询的 `/api/dsh/status`。
  3. **「server 目录在哪」只有一个入口**：`serverDirFn` —— 更新 dsh 服务、回滚备份、
     市场定位 dshmarket 都用它，测试也因此能注入临时目录（否则会碰到真实的
     `/var/apps/Harness/target/server`）。
- **dsh 的插件 bundle 带一年期 `immutable` 强缓存且无 `ETag`/`Last-Modified`** ——
  响应头为 `Cache-Control: public, max-age=31536000, immutable`；`rev` 由 dsh 自身生成、
  不随 harness 升级变化，且该路由严格校验 `rev`（改写/省略一律 404），故 URL 无法被
  harness 击穿。**普通刷新不回源**，升级后必须清除浏览器缓存或强制刷新才能拿到新字节。
  因此 `proxy.go` 里的注入刻意保持「与开关无关的恒定形态」，把开关状态放到每次回源的
  HTML 中（`window.__DSH_BROWSER_COMPAT__`），详见 README「浏览器兼容模式」一节。
  调试注入时：前端/注入改动看不到效果，先排查缓存，不要直接怀疑代码。

---

## 6. 命令速查

```bash
make dev              # 开发构建
make release V=1.0.1  # release 构建 + 版本号
make clean            # 清理产物
./build.sh            # 等价脚本

cd frontend && npm run dev      # 仅前端热更（配合后端调试）
cd frontend && npm run build    # 前端构建
```

---

## 7. 新增/修改时建议的自查清单

- [ ] 前端资源与 API 是否走 `runtimeBase()`？
- [ ] 是否硬编码了平台路径/端口？
- [ ] 涉及 dsh 进程时是否用 `effectivePID()`？
- [ ] 前端改动是否已重新构建并验证 URL？
- [ ] 新增 Pinia store / composable 是否遵循现有结构？
- [ ] 版本号改动是否经 `-ldflags` / `build.sh release V=...` 注入，而非改代码常量？