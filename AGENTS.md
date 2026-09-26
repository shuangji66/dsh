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
- `logging.go` — **唯一的日志出口**：`logInfo` / `logWarn` / `logError` 三个等级
  （`[INFO]` 白 / `[WARN]` 黄 / `[ERROR]` 红），行格式
  `[Harness] <时间> [LEVEL] message`；终端（stdout 是 TTY）额外用 ANSI 着色，日志文件
  保持纯文本，由控制台日志页按 `[LEVEL]` 着色。连续重复的同一行只记一次、序列结束时补
  汇总行（`flushLog` 在退出前补出）。dsh 子进程的 stdout/stderr 经 `dshLogWriter`
  **原样透传**（不加前缀、不做抑制），终端固定黄色。更新类日志按**目标分开打标签**：
  `[harness]`（控制台）/ `[dsh]`（dsh 服务）/ `[market]`（插件市场），见
  `updateLogTag` —— 新增加更新步骤时不要再用通用的 `[update]`，否则升级日志又会混在一起。
- `boot.go` — 启动阶段状态机（`starting/auth/deps/ready/failed/disabled`）+ `proxyState`。
  反代据此决定等待页显示什么、能否放行；阶段由 `main.go` 推进。
- `config.go` — `AppConfig`（前端可改，含反代端口 `ProxyPort`）与 `RuntimeEnv`（环境变量）。
  **反代端口是持久化配置项**（`proxyPort`，默认 `3079`），不再读 `PROXY_PORT`。
  **手动设置的 node 堆内存上限**（`dshMemLimit`，`dshMemAuto` 关闭时）低于
  `minDshMemLimitMB`（500）会让整次保存被拒（`handleSaveSettings`）；前端同阈值
  （`stores/settings.ts` 的 `MEM_LIMIT_MIN_MB`）红字提示并在 `save()` 里拦截，
  <800 只黄字提醒、不拦。自动设置不受该限制。
- `admin.go` — Admin mux（Unix socket）：`buildHandler()` 里一个大的 `switch` 分发
  所有 `/api/*` 路由；`spaHandler` 提供内嵌前端。
- `dsh.go` — `DshManager`：进程生命周期；`effectivePID`/`findDshPid` 处理**装插件自重启**
  后 PID 变化（扫 `/proc/<pid>/cmdline` 匹配 `dsh web ... --port <port>`）；
  `tokenScanner` 从 dsh 日志捕获 `?token=`；`ExchangeToken` 换 `dsh-auth-*` Cookie；
  `SessionSettled`/`markSessionSettled` 记录「本代凭据是否已换取完成」（反代放行门禁）。
- `proxy.go` — 反向代理，转发时**携带 dsh 会话 Cookie**；`ServeHTTP` 里顺序是
  「剥挂载前缀 → 鉴权路由 → 就绪状态 `/_ready` → 鉴权 → 等待页/转发」，放行判定只在
  `reverseProxy.state()` 一处；等待页（`waitingPageHTML`）轮询 `/_ready` 并自动跳转。
  反代有**两条监听、两种挂载**：TCP `proxyPort`（配置项，默认 `3079`）是根挂载（历史行为），可选的
  Unix Socket（`HARNESS_PROXY_SOCK`，默认 `$TRIM_APPDEST/dsh.sock`）挂在
  `HARNESS_PROXY_BASEURL`（默认 `/app/Harness/dsh`，刻意比控制台 baseurl 深一层）下
  ——平台网关把该子路径整段转发到 socket，由反代**剥离前缀**后转发给 dsh（dsh 只认
  `/`、`/api`、`/plugins`；其 0.1.7-alpha.1 起前端全走文档相对路径，靠
  `<base href="./">` 自动拼出 `<prefix>/api`）。挂载换算集中在 `proxyMount`
  （`strip`/`join`/`dir`/`joinURI`）。
- `terminal.go` — WebSocket + `creack/pty` 的交互式 bash。前端连 `/terminal`（`?id=` 为空
  则新建，非空则挂载既有会话：回放历史文件 + `\x1b]ready\x07` 后进入实时流），浏览器断开
  **只解挂载不杀会话**（会话继续运行并写历史临时文件）。**一个会话同时只有一个操作端
  （单挂载点）**：`Session.attach` 换主并返回被顶掉的旧连接，handler 用 `kickDetached`
  通知旧端（`\x1b]detached\x07` + close 4001）；被顶掉端的输入 / 尺寸请求在服务端丢弃
  （`writeInput` / `resize` 走 `isOwner`，非操作端返回 `errNotOwner`，上层静默忽略）。
- `update.go` — 更新 harness / dsh 服务与插件市场：版本检测、下载、备份与回滚；
  `harnessVersion` 由 `-ldflags -X` 注入。下载策略按类型分开（`downloadPlanFor`）：
  发布资产（harness/dsh）走「代理+直连各 2 次 + HTTP Range 断点续传 + 暂停/取消」，
  插件市场**只直连、不支持续传/暂停**（包小、registry 通常不需要代理）。
  **是否「先从代理更新」由 `AppConfig.ProxyUpdate`（设置页「代理更新」）控制** ——
  代理地址探测不通或代理通路失败回退直连；它与 `ProxyEnabled`（设置页「代理dsh」，
  只管 dsh 进程自身的出网环境变量，见 `dsh.go` 的 `buildEnv`）**相互独立**，别混用。
  **备份包留不留只看有没有回滚入口**：只有 `server-*`（概览页有「dsh 服务回滚」）与
  用户主动的 `dsh-data-*` 需要留存；harness 与插件市场的备份包收尾即删
  （`removeUnusedBackup`）—— 新增更新分支时没有回滚入口就别把包留在盘上。
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
- `views/TerminalView.vue` + `components/KeypadBar.vue` — Web 终端页：xterm + `/terminal`
  WebSocket；**单挂载点**（被其他设备接管时进 `detached`：写提示行 + toast，
  **不自动重连**，点「重连」= 显式夺回，见 `DETACHED_PAYLOAD` / `WS_CLOSE_DETACHED`）；
  移动端底部辅助键条 `KeypadBar`（两页：功能键/方向键/常用符号 + 常见标点；Shift 是
  **上档锁定**，见其 `SHIFT_MAP` / `SHIFT_CURSOR`；`@key` / `@toggle` 交由本页发送与
  切换修饰键，方向键长按连发）。`composables/useMobileLayout.ts`（触屏或窄视口 /
  `useWideLayout` 平板档）与 `composables/useKeypadPage.ts`（模块级共享的当前页）。
- i18n（`useI18n.ts`）只存 localStorage，不随设置持久化到后端。
- **弹窗统一结构**：右上角关闭一律用 `components/DialogCloseButton.vue`（内部是
  `.g-dialog-close`，绝对定位 `right-3/top-3` 的 X 图标），标题用 `.g-dialog-title`
  （自带 `pr-11` 给 X 让位），底部操作行用 `.g-dialog-actions`；弹窗里的动作按钮一律
  「带边框 + 不填充底色 + 同档字号」—— 普通操作用 `g-btn-secondary`、危险操作用
  `g-btn-danger`、警告用 `g-btn-warning`，**不要在弹窗里用 `g-btn-primary`**（填充色）或
  自创尺寸。新增弹窗时照这套来（列表行内的纯图标按钮仍用 `g-btn-ghost`，那是列表操作）。
- **全局禁选 / 禁原生拖拽 / 输入框禁自动填充**（`style.css` + `App.vue`）：
  `html { user-select: none }` 全局禁止文本选择，需要拖选的地方必须显式加 Tailwind 的
  `select-text` —— 现在只有两处：`LogView` 的日志 `<pre>`、`TerminalView` 整页；日志路径与
  「自动滚动」状态、插件名与版本号都刻意不放（插件名走「点击即复制」，见 `PluginsView` 的
  `copyPluginName`）。输入框 / textarea / contenteditable 已在同一条规则里放开。
  **原生拖拽**由 `App.vue` onMounted 里捕获阶段的 `dragstart` 统一拦掉 —— Chromium 把
  v-html 注入的内联 SVG 图标当图片一样可拖，随手点一下就拖出半透明拖拽快照；将来要加拖拽
  排序/拖放上传，给对应元素标 `data-allow-drag` 即可放行。所有输入框都要带 `autocomplete`
  （密码框用 `new-password`，其余 `off`）。剪贴板写入统一走 `utils/clipboard.ts`
  （http 反代访问时没有 Clipboard API，内部有 execCommand 兜底），别再各写一份。

---

## 4. 必须遵守的规则（Agent 优先）

1. **不要破坏“运行时 baseurl”机制** —— 改前端资源路径/API 时，始终经
   `runtimeBase()` / `document.baseURI`，不要硬编码前缀。
2. **不要硬编码平台路径** —— 用环境变量（`TRIM_APPDEST`、`TRIM_PKGVAR`、
   `HARNESS_*`）而非写死 `/var/apps/Harness`（除非是 `install.go`/`fnos.go` 等
   明确约定平台常量的位置）。
3. **反代端口改动必须“先绑新、再关旧”** —— 它是可持久化的配置项（`AppConfig.ProxyPort`，
   默认 `3079`，不再读 `PROXY_PORT`），保存时经 `startProxy` 同步绑定新端口：绑定失败
   （占用/无权限）必须整次拒绝保存且旧监听不动，成功后由 `startProxy` 关闭旧监听；
   只有 `startProxy` 能改监听，别另起一个监听函数或只在启动时读一次端口。
4. **PID 感知** —— 涉及 dsh 进程生命周期/状态时，用 `effectivePID()` 而非直接信任
   `m.cmd.Process.Pid`（dsh 会装插件自重启）。
5. **前端改动必须重编译验证** —— 修改前端后运行构建并刷新确认，别只改文件。
6. **遵循 Vue 最佳实践** —— 优先 Composition API；状态进 Pinia；可复用逻辑进
   composables。
7. **日志只用 `logInfo` / `logWarn` / `logError`，且用英文** —— 不要再引入
   `logger()` / `log.Printf` / `fmt.Println`。基础的成功操作（PID 文件写入、无需重装的
   空操作、回收子进程成功等）**不记日志**；只记状态变化、用户发起的操作与失败/异常。
   高频路径（状态轮询、每小时自动检测）要么只在结果真正变化时记一行，要么依赖
   `logging.go` 的重复抑制，**不要每次调用都刷一行**。dsh 子进程的输出必须原样透传，
   不要加前缀或改写格式（控制台靠「无 `[Harness]` 前缀」把它识别为黄色 dsh 输出）。
8. **保持双语注释习惯** —— 现有代码中文注释占多数，新增注释建议保持项目既有风格。
9. **一个终端会话同时只有一个操作端（单挂载点，语义不可回归）** —— 后端 `Session.conn`
   就是唯一挂载点，`attach` 换主并返回旧连接，handler 用 `kickDetached` 通知旧端
   （`\x1b]detached\x07` + close 4001）；被顶掉端的输入 / 尺寸在服务端丢弃
   （`errNotOwner`）。前端收到后进 `detached` 状态（提示 + **不自动重连**，否则两端会
   互相顶号），点「重连」= 显式夺回。不要改成「多端同时挂载」，也不要给 detached 加自动重连。
10. **终端辅助键条的显隐不能按宽度断点** —— `md:`（768px）只表示「屏幕宽」，iPad 的 CSS
   宽度是 768/834/1024px，会被判成桌面而丢掉整条辅助键（触屏上再没有 ESC/Tab/Ctrl/Alt/
   方向键）。一律走 `composables/useMobileLayout.ts`（触屏 **或** 窄视口），**不要写回
   `md:hidden`**；「平板档（两页并排）」用同文件的 `useWideLayout()`（就是 md 断点，别另发明数值）。

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
- **反代有「根挂载」与「子路径挂载」两种形态，自留路径一律经 `proxyMount`** ——
  子路径挂载来自平台网关把 `<prefix>/…` 整段转发到 `HARNESS_PROXY_SOCK`，反代在
  `stripMount` 里剥掉前缀（此后 `r.URL` 是挂载内路径），因此：
  1. **新增/修改反代自留路径（`/_login`、`/_logout`、`/_ready`）或任何 302 目标时，
     必须用 `p.mount.join(...)` / `joinURI(...)` 补回前缀** —— 直接写绝对路径在子路径
     部署下会把浏览器送出挂载目录（登录、登出、等待页轮询与 Refresh 兜底都会失效）。
     `auth.go` 的登录页表单 action 用 `__LOGIN_ACTION__` 占位符注入同一个换算结果。
  2. **不要把挂载前缀硬编码进代码**：它来自 `HARNESS_PROXY_BASEURL`（默认
     `/app/Harness/dsh`，位于控制台 baseurl 之下一层，避免与 admin socket 抢路径），
     `HARNESS_PROXY_SOCK`（默认 `$TRIM_APPDEST/dsh.sock`）为空或 `off` 时这条监听
     整体关闭。网关侧必须**按更长前缀优先匹配**，否则控制台那条规则会把 dsh 流量截走。
  3. **dsh 侧不需要 baseurl**：0.1.7-alpha.1 起其前端全走文档相对路径（`<base href="./">`），
     浏览器自己拼出 `<prefix>/api`；不要试图去改写 dsh 的产物，也不要为了“兼容子路径”
     给 dsh 传前缀参数。此机制的前提是 **dsh ≥ 0.1.7-alpha.1**，更早版本在子路径下必然 404。
- **`PROFILE_TEMPLATES.web.bundles` 注入** —— 只在 `server-build.yaml` 的 CI 中对
  `dsh-app-boot` 做，本地不涉及。
- **反代端口默认 `3079`（设置页可改，持久化在 `config.json` 的 `proxyPort`）、
  dsh 端口默认 `13080`** —— 冲突排查先看这两个；两者不可相同（设置页与后端都校验）。
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
- **iOS 上「菜单内焦点搬家」的 `relatedTarget` 是 null** —— dsh 0.1.7 的模型座位
  （`conversation.input.model`）自己实现两级菜单，并在根节点用 `onBlur` 关菜单，守卫写成
  `event.relatedTarget instanceof Node && (rootRef/menuRef 包含它)`：桌面上点选项时焦点落在该
  选项、守卫放行；**iOS WebKit 在菜单内按钮之间搬家时 `relatedTarget === null`**，守卫直接
  `close()`，菜单在 `mousedown` 与 `click` 之间被卸载 —— 选项的 `click` 没有目标、React 的
  `onClick` 不跑，也就永远没有 `session/selectModel`（现象：菜单能开、根面板两行点得动、进
  二级面板点选项毫无反应；桌面正常）。反代因此在 `bootstrapScript` 第 6 段按「触屏 + 「浏览器兼容模式」开关 + 模型
  座位自己的菜单（`aria-expanded` + `aria-controls`）」把它内部的 `focusout` 拦在捕获阶段，
  桌面不武装，见 README「移动端模型 / 推理等级菜单（iOS）」。
  排查同类「点了没反应」时的两条经验：**先看 `window.__DSH_MODEL_MENU_FOCUS_GUARD__` 是否
  武装**；诊断打点必须能区分「事件没到元素」与「事件到了但元素随即被卸载」——只看服务端有没有
  收到请求，会把后者误判成前者（本次就是因此先误修了一轮）。
- **真机诊断打点（默认关闭，`HARNESS_DSH_DIAG=1` 或页面 URL 带 `?dsh-diag=1`）** ——
  只在真机上出现的问题（事件被吞、元素在两次事件之间被卸载、focus 语义与桌面不同）用「服务端
  有没有收到请求」是查不出来的：本次「iPhone 点模型选项没反应」一开始就是因此误判成「事件没到
  元素」，白改了一版。内置打点在 `proxy.go` 的 `dshDiagScript`（默认不注入，见 `dshDiagEnabled`），
  它把模型座位与菜单相关的现场以 **URL path** 形式打回本站
  （`<prefix>/dsh-diag/<事件>/<分片>/<数据>`，dsh 回 404、无副作用，但会落进
  `/usr/trim/nginx/logs/access.log`）—— 不需要新开后端接口、不需要真机调试器。
  事件：`s` 状态快照与 10s 心跳（视口 / 座位 `disabled` / 座位中心 `elementFromPoint` 命中谁 /
  编辑面是否可编辑 / 菜单矩形 / 滚动位置）；`e` 命中模型座位或菜单的指针事件；`d` 菜单打开期间的
  `mousedown` / `click` 明细（命中链 / 命中点元素 / **被按节点是否仍连接**）；`m` 菜单出现与消失
  （消失时带最近 10 条事件尾巴 —— 判断「谁把菜单关掉的」）；`o` 菜单 portal 节点被新建/移除；
  `f` 焦点变化（含 trigger 与 `relatedTarget` —— iOS 的关键）；`n` 座位节点是否被替换；
  `x`/`r` JS 报错与未处理拒绝。解读（把 URL path 还原成 JSON）：

  ```python
  # python3 read-diag.py [since HH:MM:SS] —— 按到达顺序合并分片，逐条打印
  import re, json, urllib.parse, sys
  since = sys.argv[1] if len(sys.argv) > 1 else "00:00:00"
  cur = None
  for line in open("/usr/trim/nginx/logs/access.log", encoding="utf-8", errors="ignore"):
      if "dsh-diag/" not in line:
          continue
      t = re.search(r"\[(\d{2}/\w{3}/\d{4}:(\d{2}:\d{2}:\d{2}))", line).group(2)
      m = re.search(r"dsh-diag/(\w+)/(\d+)-(\d+)/(\S+?)\s", line)
      if not m or t < since:
          continue
      kind, idx, total, data = m.group(1), int(m.group(2)), int(m.group(3)), m.group(4)
      if idx == 1 or cur is None:                      # 每个分片组的第一个分片起一条新打点
          cur = {"t": t, "kind": kind, "n": total, "c": {}}
      cur["c"][idx] = data
      if len(cur["c"]) == cur["n"]:                    # 分片齐了再解码
          payload = "".join(cur["c"][i] for i in sorted(cur["c"]))
          print(cur["t"], cur["kind"], json.loads(urllib.parse.unquote(payload)))
          cur = None
  ```

  （上面这段是最小可用版本：分片没齐时解码会抛错，跳过即可；需要严格版本时按「idx==1 起新组、
  后续 idx 递增补齐」重组。）**排查结论要落在「事件是否到达目标」与「目标是否还在文档里」两问上**，
  不要只看有没有发出请求。
- **终端辅助键条：显隐按「触屏 or 窄视口」，两页靠按钮切换（不做滑动），Shift 是上档锁定** ——
  iPad 的 CSS 宽度 ≥768px，用 `md:hidden` 会整条丢掉键条（触屏上就没有 ESC/Tab/Ctrl/Alt/方向键），
  故显隐一律走 `useMobileLayout()`；平板档（`useWideLayout()`，≥768px）**两页并排**显示、
  不显示切页按钮。手机档单页：第一页只在**右侧**显示「›」、第二页只在**左侧**显示「‹」
  （两侧不同时出现，结构上不存在循环、不支持滑动切页），`useKeypadPage().step(±1)` 夹取不循环，
  页状态是**模块级共享**的（切页面/切视图再回来不跳回第一页）。Shift 锁定时符号键发上档字符
  （`SHIFT_MAP`）、方向键变 Home/End/PageUp/PageDown（`SHIFT_CURSOR`，键面 HM/ED/PU/PD），
  **键面文字与发出的字符必须一起变**（只改颜色或只改文字都是 bug）；因此 `TerminalView` 的
  「修饰键输入一次后自动解除」**必须排除 shift**（只清 ctrl/alt）。根节点的
  `@touchstart.prevent.stop` 要保留：它阻止浏览器把触摸合成为鼠标事件与长按菜单，单次按键的
  `@click` + `@touchstart.prevent` 双保险依赖它。验证时按**可见**判定
  （`getBoundingClientRect().height > 0`）——旧的 `md:hidden` 只隐藏不卸载，只看 DOM 会假阳性。
- **移动端终端：触摸滚动是前端自己实现的，键盘弹起时还要锁死文档滚动** —— 两者都不能改回
  「交给浏览器」：
  1. **xterm 不处理触摸滚动**：它只监听 wheel/鼠标；那个可滚动的 `.xterm-viewport` 是屏幕层
     `.xterm-screen` 的**兄弟节点**，手指落点永远不在它身上，浏览器只能顺着祖先链去滚**整个
     文档** —— 现象就是「终端内容不滚、整页（含辅助键栏）被拖着上滚」。所以
     `TerminalView.onTouchMove` 自己把手指位移按行高（`.xterm-screen` 高度 ÷ `term.rows`）换算成
     行数调 `term.scrollLines()`（**正数 = 往新内容方向**，与自然滚动同向；只把整行位移记进基准，
     余量留给下一次 move），判定为竖向滚动后 `preventDefault()` 接管这次手势；`.term-container`
     上的 `touch-action: none` 则在触摸起始就关掉浏览器的平移/缩放手势。**三条缺一不可**：删掉
     preventDefault 或 touch-action 都会让拖拽重新升级成「滚文档」，改用原生滚动则永远不会生效。
     长按粘贴菜单（600ms）与它的「位移 >10px 取消」判定必须共存：滚动判定只看竖向主导的位移。
  2. **键盘弹起时要锁文档**：`html[data-kb]` 下终端页高度已是可视视口高度（`--vv-h`），但 App 根节点
     的 `min-h-screen` 与 `main` 给键盘预留的 `padding-bottom`（`--kb-inset`）让文档仍比可视视口
     高出一大截，手指一拖照样把整页带走、辅助键栏随即离开键盘顶边。`style.css` 用
     `html[data-kb]:has(.terminal-page)` 把 html/body 的 `overflow` 锁成 `hidden`
     （外加 `overscroll-behavior: none` 断链式滚动）。`:has()` 限定范围很重要：终端页被 `KeepAlive`
     切走后其 DOM 不在文档里、选择器自然不匹配，其它页面键盘弹起时**仍然要能滚动**到被键盘挡住的输入框。
     验证（无头 Chrome + CDP 触摸事件即可复现）：手机视口下沿终端上下拖动，`.xterm-rows` 文本要变化
     且 `window.scrollY` 恒为 0；注入 `data-kb` + `--vv-h` 后键栏底边必须正好落在 `--vv-h` 处，
     `document.scrollingElement.scrollTop` 强设为 200 也要被钳回 0。
- **单挂载点（一个终端会话只在一台设备上进行）** —— `Session.attach()` 回放历史 + 发 ready 后
  在 `connMu` 内**原子换主**并返回旧连接；`kickDetached()` 给旧连接发 `\x1b]detached\x07`（先）
  与 close 4001（后，双保险——帧被代理吞掉也能靠码判定），写带 1s 超时，**绝不能让这次写阻塞
  新端挂载**。前端 `detached` **不自动重连**（否则两端互相顶号），「重连」= 显式夺回；顶号只
  作用于该会话，同一设备其他连接不受影响。验证：假连接单测 attach / isOwner / detach /
  kickDetached 语义（`backend/terminal_attach_test.go`）；沙箱里 PTY 建不起来
  （`/dev/ptmx: permission denied`），真会话必须部署后真机验证（两台设备 / 两个浏览器上下文
  互相顶号，且被顶端不自动重连）。
- **dsh 的插件 bundle 带一年期 `immutable` 强缓存且无 `ETag`/`Last-Modified`** ——
  响应头为 `Cache-Control: public, max-age=31536000, immutable`；`rev` 由 dsh 自身生成、
  不随 harness 升级变化，且该路由严格校验 `rev`（改写/省略一律 404），故 URL 无法被
  harness 击穿。**普通刷新不回源**，升级后必须清除浏览器缓存或强制刷新才能拿到新字节。
  因此 `proxy.go` 里的注入刻意保持「与开关无关的恒定形态」，把开关状态放到每次回源的
  HTML 中（`window.__DSH_BROWSER_COMPAT__`），详见 README「浏览器兼容模式」一节。
  调试注入时：前端/注入改动看不到效果，先排查缓存，不要直接怀疑代码。
- **控制台自己的前端资源缓存策略**（`admin.go` 的 `serveBytes`）—— 与上面 dsh 那套刻意相反，
  按「文件名有没有内容哈希」分成两类，**不要合并**：
  - `assets/**` 是 Vite 的内容哈希产物 → `public, max-age=31536000, immutable` 长期强缓存
    （首屏几个百 KB 的 bundle 不再每次回源）。文件名随内容变，所以升级后拿到的必然是新字节；
  - `index.html` / `callback.html` 等入口文件（名字不带哈希）→ `no-cache` + 内容摘要 `ETag`
    （命中回 304）。**别给入口文件加强缓存**：缓存住它就挡住了后续版本的资源名切换，表现为
    「升级了控制台还是旧界面」——与上面 dsh 插件 bundle 同一个坑。
  内嵌 FS（`//go:embed`）没有可用的修改时间，校验器只能用内容摘要 ETag；`spaPath()` 会把
  无扩展名的 SPA 路由都映射到 `/index.html`，因此前端路由回退也走「不缓存」那条分支。
- **目录页的宿主桥接必须在「页面加载时」握手（`@trimjs/web-app` 的 60 秒窗口）** ——
  现象：打开控制台一段时间后再进目录页，「添加授权目录 / 打开」点了毫无反应（不报错、
  不弹 toast）；关掉应用窗口重开、马上进目录页又正常。成因链：
  1. 飞牛桌面（宿主）挂载应用 iframe 时建立 postmate 连接，**握手超时写死 60 秒**
     （宿主前端包 `n1e({ ..., timeout: 60 * 1e3 })`，包在 `/usr/trim/www/assets/index-*.js`）；
     超时即 `destroy()` 掉自己的 `message` 监听 —— 之后子页再发 SYN 永远没人应答。
  2. 子页 SDK 的初始化链在宿主无应答时**不会失败**：1.5s 宿主探测 → 5s 扩展宿主探测 →
     一条**无超时、永不 settle**的兜底连接，且该结果按模块缓存。
  3. 于是 `sdk.ready()` / `openFileManager()` / `pickUserFile()` 全部永久 pending ——
     这不抛错，所以既没有 toast 也没有日志，表现就是「按钮没反应」。
  硬约束：**SDK 单例在 `main.ts` 的 `primeTrimApp()`（`utils/trimApp.ts`）里创建，页面一
  加载就握手**；视图里不要再 `new TrimApp()`（等视图挂载才握手就晚了）；等待桥接一律走
  `trimAppReady()`（带超时，超时给用户「关闭并重新打开本应用」的提示）。刷新 iframe 救不回来
  （宿主侧的监听已经没了），只能回桌面重开应用窗口。
  排查同类问题：宿主打开文件管理器时会打 `GET /app/token` 与 `/websocket?type=file`，
  `/usr/trim/nginx/logs/access.log` 里「进了目录页却没有这两条」就是握手已失效的现场。

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