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
  → 起 dsh（非 `HARNESS_AUTOSTART=0`；**先查有没有选中的 dsh 版本**，没有就进
  `not-installed` 而不是报「启动失败」）→ 换 Cookie → 装 node-pty → 标记就绪 → 等信号退出。
- `logging.go` — **唯一的日志出口**：`logInfo` / `logWarn` / `logError` 三个等级
  （`[INFO]` 白 / `[WARN]` 黄 / `[ERROR]` 红），行格式
  `[Harness] <时间> [LEVEL] message`；终端（stdout 是 TTY）额外用 ANSI 着色，日志文件
  保持纯文本，由控制台日志页按 `[LEVEL]` 着色。连续重复的同一行只记一次、序列结束时补
  汇总行（`flushLog` 在退出前补出）。dsh 子进程的 stdout/stderr 经 `dshLogWriter`
  **原样透传**（不加前缀、不做抑制），终端固定黄色。更新类日志按**目标分开打标签**：
  `[harness]`（控制台）/ `[dsh]`（dsh 服务）/ `[market]`（插件市场），见
  `updateLogTag` —— 新增加更新步骤时不要再用通用的 `[update]`，否则升级日志又会混在一起。
- `boot.go` — 启动阶段状态机（`starting/auth/deps/ready/failed/disabled/not-installed`）
  + `proxyState`。反代据此决定等待页显示什么、能否放行；阶段由 `main.go` 推进。
  `not-installed` 表示「本机没有选中的 dsh 版本」，等待页据此提示去概览页下载 + 切换
  （`userActionPhase` 是「需要用户动手」的唯一判定入口，新增阶段时一起改）。
  **这个阶段会在「已选中且装着版本」时自动作废**（`proxy.go` 的 `state()`）：启动阶段只有
  `main.go` 能写，而控制台起来之后用户在概览页装好版本并点了「切换」是常态 —— 不作废的话
  等待页会一直说「尚未安装 dsh 服务，请先下载一个版本」，与事实相反（切换后启动失败时更糟：
  真正的原因只在日志里）。
- `config.go` — `AppConfig`（前端可改，含反代端口 `ProxyPort`）与 `RuntimeEnv`（环境变量）。
  `AppConfig.DshVersion` 是**当前选中的 dsh 版本**（`${TRIM_PKGVAR}/server/` 下的目录名，
  空 = 未安装）：由概览页的版本列表写入，设置页不暴露；这是全应用唯一的 dsh 版本来源。
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
- `update.go` — **harness 控制台自更新**（版本检测、下载、备份与替换）+ dsh 数据备份/
  恢复；`harnessVersion` 由 `-ldflags -X` 注入。**自更新只允许同一 major.minor 线内的
  小版本（1.4.3 → 1.4.9）**：跨主要/次要版本（1.4 → 1.5、1.x → 2.x）一律拒绝下载与安装，
  改为提示需更新 fpk 安装包 —— `HasUpdate` 的语义就是「有可就地下载安装的
  更新」，跨线走 `StoreUpdate`/`StoreVersion`，判定入口是 `harnessUpdateTarget` /
  `sameMinorLine`，见规则 14。下载只有「发布资产」一种策略
  （`downloadPlanFor`）：代理+直连各 2 次 + HTTP Range 断点续传 + 暂停/取消；
  **是否「先从代理更新」由 `AppConfig.ProxyUpdate`（设置页「代理harness更新」）控制** ——
  代理地址探测不通或代理通路失败回退直连；它与 `ProxyEnabled`（设置页「代理dsh」，
  只管 dsh 进程自身的出网环境变量，见 `dsh.go` 的 `buildEnv`）**相互独立**，别混用。
  **备份包留不留只看有没有回滚入口**：现在只有用户主动的 `dsh-data-*` 要留存，
  harness 的备份包收尾即删（`removeUnusedBackup`）—— 新增更新分支时没有回滚入口就别把
  包留在盘上。发布资产还要过 **sha256 校验**（`releaseChecksum` / `verifyFileSHA256`）：
  校验文件是 Release 里与包同名的 `.sha256`，**静默校验**（不写更新状态、不记成功日志），
  失败才进更新弹窗并删掉坏包；**缺失/取不到只记 WARN、不阻塞更新**。
  **dsh 数据备份是异步的**（`BackupDshData` + `dshBackup` 跟踪器）：接口只做校验后立刻
  返回，打包在 goroutine 里跑，进度走 `GET /api/dsh/backup/status`、取消走
  `POST /api/dsh/backup/cancel`（取消删除不完整的备份文件）—— **别把 handler 改回同步**，
  前端「关掉弹窗不终止备份、重开弹窗同步进度」全靠这条边界。打包只有一份实现
  `tgzDirAsProgress`（`tgzDirAs` 是它的 `prog=nil` 包装），与「更新/恢复/重置插件」用
  同一把 `applying` 双向互斥（`beginExclusiveDshDataOp` 是短操作的取用入口）。
  三条配套约束：
  1. **产物分两步落盘**：先写 `<名称>.part`、成功后 `os.Rename` 成 `<名称>`（见
     `backupPartSuffix`）。恢复是「先删 `~/.dsh` 再解压」，半成品若与完成包同名，一次
     进程被杀就能让用户用半个包恢复、把数据删了却解不出内容。`.part` 不会被备份列表
     列出（`isBackupFile` 只认 `-<14位时间戳>.tar.gz`），启动时与每次开始备份前由
     `sweepPartialBackups` 清掉。**别再改回直写最终文件名。**
  2. **预扫描（`scanDirSize`）在 goroutine 里做**，接口返回前只跑存在性与互斥检查；
    总量先为 0（前端按「总量未知」渲染），扫完由 `dshBackup.setTotals` 补上。
  3. `DeleteDshDataBackup` 拒绝删除**正在写的那一份**（否则备份仍报成功、盘上却查无此包）。
  **dsh 服务更新与插件市场更新都不再走这里**（没有可下载的压缩包）。
- `server.go` — **dsh 服务的多版本管理**：`${TRIM_PKGVAR}/server/<版本>/` 下的
  `npm install --prefix` 安装产物；镜像源取自 `npmMirrors`（阿里云 → 腾讯云 → 华为云，
  不试官方源），版本列表按 `dshMinVersion`（**0.1.7-rc.1**）过滤，下载/删除/切换与
  「未安装」状态都在这里。**版本号会变成目录名与 npm 参数**，一律先过
  `validVersionArg`（与市场版本共用；`newestVersion` 取「版本号最高的一版」，见 market.go）。
  **「有更新」的红点只看版本号大小**（列表里最高的一版比选中的高就亮），不区分 `dist-tags`
  —— 基准是列表最高版本（`newestOf`），标签只作为列表里的标注展示，见 `refreshDshStatus`。
  启动与 CLI 用选中版本的**绝对路径**（`DshManager.dshBinPath`）。
- `market.go` — **插件市场（dshmarket）**：它是普通的 profile 插件（不是 dsh 包自带的
  bundle），检测靠 `dsh plugin --profile web list`，安装/更新/卸载靠
  `dsh plugin --profile web add|remove`，镜像源与 `server.go` 同一套；没有备份/回滚。
  这里还放着所有「停 dsh 前」共用的忙守卫（`replaceBusyGuard` / `stopDshForReplacement`）。
  **「查不到」不等于「未安装」**：那条检测命令在 dsh 就绪前必然失败，失败时置
  `UpdateStatus.Unavailable`（前端版本行显示「—」、市场弹窗不给安装入口），**保留**上一次
  已知的版本号，并且只有命令成功退出才写 `LocalVersion`（空 = 真没装 →「未安装」）。
  本地检测结果**只能**经 `applyMarketLocalResult` 写进状态（别在各处再写一遍
  `st.LocalVersion = snap.Version`，那会把「查不了」写成「未安装」）；检测失败的结果
  不进缓存。既然「查不到」是常态，每个「dsh 就绪」的时刻都要经
  `RefreshMarketAfterDshStart()` 补查一次（启动流水线收尾 / 概览页启动·重启 dsh /
  切换 dsh 版本），否则「—」会挂到下一次小时级检测。回归测试见
  `backend/market_unavailable_test.go`。
- `release-notes` — **两条链路取日志的路子刻意不同，别混**：
  1. **dsh 版本列表**（`update.go` 的 `notesIndex` + `/api/dsh/versions/notes`）：取上游
     `deepseek-ai/deepseek-harness` 的 GitHub Release 正文（tag `dsh-v<版本>`，见
     `dshReleaseTag`）。它是「点某个版本号看那一版」的列表，因此**按版本号索引**：把
     **最近 `dshNotesReleases`（10）个 release 一次拉全**并按版本号建表，之后每次点击都命中
     缓存。不是「一个版本查一次 API」（未认证的 GitHub API 只有 60 次/小时，连点几下就撞
     限流），也**不是把整个仓库的 release 都拉回来**（只覆盖最近 10 个版本，够用：版本列表
     本身也只列最近这些版本）。`Lookup` 的 `loaded` 标志才是「命中不了就是没有」的依据
     （只按 map 命中判会把未知版本反复重查），拉取失败**不置 loaded、也不写缓存**，所以重试
     仍能拿到内容。
  2. **插件市场**（`market.go` 的 `refreshMarketStatus`）：与 harness **完全同一套** —— 只取
     「要装的那一版」（镜像源上的最新版）的正文，按 tag `v<版本>`（`marketReleaseTag`）
     **单次请求**，写进 `UpdateStatus.ReleaseNotes` 随状态一起推送；**没有索引、没有单独的
     接口、也没有「点版本号才显示」**（前端把它内联在更新弹窗里，和 harness 一样）。
     只在确实有更新时才拉（没更新时弹窗不显示日志块）。
  **`available=false` 与「空正文」是两回事**（仅 dsh 那条有 available 字段）：前者是**这次
  没取到**（断网/限流，前端让用户重试），后者是拉到了、但该版本确实没有 release 正文 ——
  混成一句会让用户白等或白点。dsh 的正文是**中英双语**（`<h3 id="cn-…">` / `<h3 id="en-…">`
  两段），`splitBilingualNotes` 按**锚点 id** 切分（不按标题文字：那是会变的展示文本），
  英文段落缺失时留空、由前端按界面语言回退中文；dsh 正文统一用 `stripHTMLToText` 压成纯文本
  （前端 `whitespace-pre-wrap` 直出，不解析 HTML），而 harness / 市场那份是**原样 Markdown**
  （前端 `MarkdownText` 渲染）。回归测试见 `backend/release_notes_test.go`。
- `install.go` — 自动安装并 patch `node-pty`（等待 `$HOME/.dsh/profiles/web` 目录）。
- `auth.go` / `visitors.go` / `sse.go` — 登录鉴权、访客跟踪（SSE 推送）、事件流。
- `quickcmds.go` — 终端快捷指令持久化（数据目录下的 `quickcmds.json`）。
- `fnos.go` — fnOS open-gateway 客户端（`/var/run/trim_open_gateway_apiscope.socket`）。

**前端（Vue 3，`frontend/`）**

- **强制 Composition API + `<script setup>` + TypeScript**（Vue 3，SSR 下用 Volar / vue-tsc）。
- 子页面统一经 `/` 下的 `?view=xxx` 查询参数切换（`router/index.ts`），避免真实历史
  记录；旧 `/directory` 等路径做 302 重定向。**默认页跳转（`App.vue` 的 `applyDefaultView`）
  必须等 `router.isReady()` 再读 `route.query`** —— 初始导航（含懒加载子页面 chunk）解析
  完成前 query 还是空的，那时判断会把 `?view=logs` 这类直达链接（含旧路径 302 的目标）
  覆盖成默认页。
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
  **弹窗正文只分两档，别自创第三档**：主要信息（状态行 / 说明句 / 正文段落 / 更新内容）用
  `text-sm` + `text-ink-soft dark:text-[#A6A6AD]`（多行配 `leading-relaxed`）；次要信息
  （字节数、版本号等元信息、小标题、标签）用 `text-xs` + `text-ink-faint dark:text-[#8A8A92]`。
  更新弹窗的 release 正文曾写成 `text-xs`（比同屏的版本行还小），现已与其它弹窗同档。
  **更新类弹窗的标题右侧要挂对应仓库的 GitHub 裸图标**（`components/GithubIconLink.vue`）：
  标题与图标同处一个 `flex items-center gap-2 pr-11` 的行里，标题用 `.g-dialog-title !pr-0`
  （把 `.g-dialog-title` 自带的 `pr-11` 让给外层容器，否则图标会被推到 X 底下），图标不套
  `g-btn-*`。三个目标的仓库地址**只有一份**，在 `constants/repos.ts`（`repoSlugs` / `repoURL`）
  —— 弹窗是三个独立组件（harness 在 `UpdateSection`、dsh 在 `ServerVersionsDialog`、市场在
  `MarketDialog`），地址别再各写一份。dsh 版本弹窗与市场弹窗是后来拆出来的，曾整段漏掉图标，
  新增弹窗时记得一起挂上。
  **「更新日志」弹窗**（`components/ReleaseNotesDialog.vue`，**只有 dsh 版本列表在用**；
  插件市场的日志和 harness 一样内联在更新弹窗里，不走这个组件）：把版本号做成**带下划线的
  按钮**（悬停变品牌色 = 全站「可点」的统一提示，与概览页的版本行一致），点开一个只读弹窗
  —— 它**只有右上角的关闭按钮**、没有底部操作行（没有任何可执行动作），正文按纯文本展示
  （`whitespace-pre-wrap`，后端已把 release 里的 HTML 压平）。取数 / 缓存 / 「迟到的响应不
  覆盖当前内容」都在 `composables/useReleaseNotes.ts`，**成功才缓存**（限流/断网那次不记，
  否则用户重试也永远看不到内容）。空文案必须写明「只覆盖最近 10 个版本」（`release_notes_empty`）。
  **更新内容的内联展示**（harness 在 `UpdateSection.vue`、插件市场在 `MarketDialog.vue`，
  两处结构刻意保持一致）：`v-if="hasUpdate && releaseNotes"` → 次要小标题
  `update_release_notes` + `max-h-44 overflow-y-auto` 的盒子 + `MarkdownText`；正文用
  `text-sm` + `ink-soft` + `leading-relaxed`（见上面「正文只分两档」）。没有正文时整块不渲染。
  **弹窗打开时必须锁背景滚动**：`useBodyScrollLock(可见性)`（`composables/useBodyScrollLock.ts`，
  模块级引用计数 —— 叠加的弹窗/二次确认各加一层，全部关闭才解锁），否则移动端能拖拽弹窗
  背后的控制台页面。实现是「body 固定定位 + 负 top 抵消」，**别退化成给 html/body 加
  `overflow: hidden`**（本项目是 height:100% 布局，那样背景会跳回顶部）。`ServerVersionsDialog`
  与 `MarketDialog` 曾漏接（`UpdateSection` 的注释还误以为它们自己会锁），现已补上。
  **弹窗里的长信息要按「组合」换行**：dsh 版本行是「版本号 + dist-tags + 安装/使用状态」，
  窄屏放不下时要求**版本号独占一行、标签与状态整组落到下一行** —— 做法是版本号加
  `whitespace-nowrap`（否则会在连字符处折断成两行），并把标签+状态包进**同一个 flex 项**
  （父级 `flex-wrap` 只按整项换行，不会出现版本号与某个标签各占半行）；容器的 `min-w-0`
  必须保留，否则撑不下时不会换行而是横向溢出。极窄（如 375px 且同时有「切换 + 删除」
  两个按钮、两个标签）时标签与状态自身仍可能再折一行 —— 空间确实不够，不是排版 bug。
- **移动端（<md）页面滚动在 `main` 上，不在文档上** —— `main` 高度 = `100dvh − var(--bottom-nav-h)`、
  `overflow-y: auto`（`App.vue`）。原因是**滚动条归属滚动容器**：文档滚动时它贯穿整个视口，
  滑到底（overlay 滚动条也一样）会压在 fixed 底栏上。改这块要连带三处：
  1. `main` 上的 `data-scroll-lock` 是 `useBodyScrollLock` 的锚点 —— 锁文档那套对元素级滚动
     容器无效（弹窗遮罩不可滚动，手指在上面拖会滚动 `main`），所以锁定时会一起冻住它并还原
     滚动位置；桌面端 `main` 是 `overflow: visible`，composable 会自动跳过。
  2. 终端页 / 日志页自带 `h-[calc(100dvh_-_var(--bottom-nav-h))]`，与 `main` 等高，因此移动端
     **不需要**再为底栏留底部空白（原来那句 `pb-[calc(var(--bottom-nav-h)_+_var(--kb-inset))]`
     已去掉，只剩 `pb-[var(--kb-inset)]` 给键盘让位）。
  3. 键盘弹起 + 终端页在场时 `html[data-kb]:has(.terminal-page) main { height: auto }`
     （见 `style.css`）。**用 `height: auto` 而不是 `overflow: hidden`** —— 某些机型上
     `--vv-h` 比 `main` 的内容盒（`100dvh − --kb-inset`）略大，裁剪会把底部辅助键栏切掉。
  桌面端维持文档滚动：`md:h-auto md:overflow-visible md:pb-0`。验证脚本量三件事即可：
  文档 `scrollHeight === innerHeight`（不滚）、`main` 底边 === 底栏顶边、弹窗打开时
  `main` 的 `overflow` 变 `hidden` 且滚动位置不变（关闭后还原）。
- **全局禁选 / 禁原生拖拽 / 输入框禁自动填充**（`style.css` + `App.vue`）：
  `html { user-select: none }` 全局禁止文本选择，需要拖选的地方必须显式加 Tailwind 的
  `select-text`。现在加了的：`LogView` 的日志 `<pre>`、`TerminalView` 整页，以及弹窗里
  **错误/诊断类文本块**（`MarketDialog` 的失败提示、检测诊断与 npm 尾行；
  `ServerVersionsDialog` 的列表拉取失败提示、安装失败提示与 npm 尾行）—— 这些内容常要
  拿去搜/贴，不让选中等于逼用户手抄。刻意**不放**的：日志路径与「自动滚动」状态、插件名与
  版本号（插件名走「点击即复制」，见 `PluginsView` 的 `copyPluginName`）。输入框 /
  textarea / contenteditable 已在同一条规则里放开。
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
   - **后端自己产生的文件一律落在统一数据目录**（`HARNESS_DATA_DIR`，默认
     `$TRIM_PKGVAR`）：日志、PID、`config.json`、`session.key`、终端会话镜像、
     更新备份/待安装包都在 `RuntimeEnv` 里由它派生（`config.go` 的 `dataDirFromEnv` /
     `dataPath`）。**不要再新增「单独设置某个文件路径」的环境变量**（`HARNESS_LOG_FILE`、
     `HARNESS_SESSION_DIR` 等已全部移除），也不要另写一份路径拼接。pnpm 目录是唯一例外，
     仍由 `PNPM_HOME` 单独指定。
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
   - **会进日志的错误串必须是英文**：判别方法很简单 —— 这个错误会被 `logXxx(..., err)`
     打印吗？会就写英文。已经踩过的三个点：拉镜像源元数据、`verifyInstall` 的校验失败、
     `patchAttachmentFsync` 的补丁失败，都是中文串顺着 `logWarn(..., err)` 进了日志（现已
     改成英文）。
   - 守卫：`backend/logging_test.go` 的 `TestNoChineseInLogCallText`（日志调用的实参不得含
     非 ASCII）与 `TestLoggedErrorsAreEnglish`（那条链路上会进日志的错误串必须纯 ASCII）；
     镜像源中文名的守卫在 `backend/mirror_log_language_test.go`。
   - **界面文案的英文由「后端给 code + 参数、前端查字典」提供**，见下面第 13 条。
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
11. **发布资产的名字不能随便改，`.sha256` 必须与包同名（现在只对 harness 生效）** ——
   harness 的自更新按「资产地址 + `.sha256`」拼校验文件地址（`backend/update.go` 的
   `checksumURL`），`harness-build.yaml` 也按同一规则生成并上传。改资产命名（`assetURL`）
   却漏改一侧，**不会报错**，只会让校验静默退化成「不校验」（缺文件按策略只记 WARN）。
   新增发布资产时：①`assetURL` 的命名 ②workflow 的 `sha256sum` ③artifact/Release 的
   上传清单，三处一起改。harness 的格式只有 `.tar.gz`。
   - **dsh 服务与插件市场都没有发布资产了**：dsh 由控制台 `npm install` 官方 npm 包
     （`server.go`），市场由 `dsh plugin add`（`market.go`）。旧版本的 `server-*` 压缩包与
     `.tar.xz` 解码通路已整体删除（`extractTarXz` / `github.com/ulikunitz/xz` 都不在了，
     设备上不再需要 `xz-utils`）；`server-build.yaml` 只是留着未动，**新版控制台不读它**。
   - **本地备份恒为 `.tar.gz`**（`tgzDir` / `tgzDirAs`），解压走 `extractTarGz`
     （`extractArchive` 只是「下载包用哪个解压器」的唯一判定点）。不要把备份改成别的格式。
12. **dsh 版本安装 / 插件市场安装的三条硬约束（改这两条链路前必读）** ——
   - **镜像源顺序与「不重试」**：`npmMirrors` 是阿里云 → 腾讯云 → 华为云，同一个源失败就换
     下一个（整包重跑），三个都失败才报错，**不试官方 registry.npmjs.org**。dsh 安装用
     `--fetch-timeout=5000 --fetch-retries=0`（「5 秒没反应就换下一个」就是这个语义：
     单请求 5 秒拿不到响应即失败）；插件市场的**安装/更新**（`add`）用 `--registry=<镜像源>`
     逐个尝试。**`remove` 绝对不能带 `--registry`** —— pnpm 的 remove 没有这个选项，带上
     会以 `Unknown option: 'registry'` 失败，而错误信息还会误导成「镜像源都失败了」；
     卸载走 `runMarketPluginCmdOnce`（执行一次、不带 registry、不回退）。
   - **镜像源的中文名只进界面，日志与错误链一律用英文标识**（`npmMirror.Slug`：
     aliyun / tencent / huawei，取用入口 `mirrorLogName()` / `mirrorLogNames()`）——
     日志必须全英文（规则 7），而中文显示名（阿里云/腾讯云/华为云）过去既进界面也进日志，
     还会顺着「插件市场操作失败（…均失败）」这类错误被 `logError(..., err)` 原样打出去。
     因此 `Name` 只用于进度文案与 `DshInstallState.Mirror` 这类界面字段，日志/错误里用 Slug；
     回归测试见 `backend/mirror_log_language_test.go`（含一条「日志调用不得引用
     `mirror.Name`」的源码守卫）。
   - **插件市场装的是「版本号最高的一版」，不是 `dist-tags.latest`**：`latest` 是手动标签，
     会滞后或指向另一条线。检测与安装都走 `newestVersion`（版本号排序取最高），安装命令是
     `add dshmarket@<精确版本号>`（**不要写回 `@latest`**）。**dsh 版本行的红点现在也是同一套
     语义**（用户要求「不区分标签通道，版本号大就提示」）：基准改成列表里最高的一版
     （`newestOf`），列表里仍标注 latest/alpha/next，但标签不参与判定 —— 别再改回
     `dist-tags.latest`。
   - **版本列表 / 市场本地检测是「缓存优先」，不要顺手加回 TTL 或改成每次都查**：
     `refreshVersions(force)` 与 `detectMarketLocal(force)` 在 `force=false` 时只要有缓存就
     直接返回（不联网、不起子进程）；刷新的入口只有「弹窗里的刷新」「后台自动检测 / 手动检查」
     与「市场装/卸完成」三处。这两个查询一个要打三个镜像源、一个要起 1~3 秒的
     `dsh plugin list` 子进程，被状态重算/页面打开这些高频场合反复触发过。
     市场本地检测有两个**刻意**的例外（见上面 `market.go` 那条）：检测**失败**的结果不写进
     缓存，且每个「dsh 刚就绪」的时刻补查一次 —— 否则「查不了」会被缓存住、界面一直显示「—」。
   - **附件 fsync 补丁必须在 `npm install` 之后由控制台补上**（`patchAttachmentFsync`）：
     dsh 的 attachment-local 会逐级上溯 fsync 到 `/`，而 fnOS 的 `/vol1`、
     `/vol1/@appshare` 是 mode 000（trim_acl 只给穿越），非 root 读不了 → `EACCES` →
     **WEB 端上传文件/图片整体失败**。锚点缺失时只记 WARN（不阻塞安装），但绝不会静默通过。
   - **顺序**：市场安装/卸载是 pnpm 在 profile 里跑并持有 profile 写锁，**绝不能在它跑的时候
     停 dsh**（会留下陈旧锁）。所以是「先跑完插件命令 → 过忙守卫 → 重启 dsh 让 bundle 生效」；
     切换 dsh 版本同样是「过忙守卫 → 停 dsh → 用新版本启动」。
   - **控制台侧所有 `dsh plugin …` 都走 `DshManager.RunPluginCommand`**（超时由调用方给：
     插件页的只读命令用 `pluginCmdTimeout`、市场 `add` 用 `marketCmdTimeout`）。它做两件事：
     执行前清陈旧锁、全程登记 `PluginCmdRunning()`（忙守卫与概览页风险提示的来源）。第 5 节
     那条硬约束里有这条的完整来龙去脉与回归测试位置。
   - **换镜像源重试前必须把版本目录重建出来**（`resetVersionDir`）：`npm install --prefix <dir>`
     的目录不存在时 exec 直接 chdir 失败，那次换源就白丢了，错误还会被算到镜像源头上。
     npm 失败与 verify 失败两条分支都要重建（回归测试
     `TestInstallFallbackAfterVerifyFailureKeepsUsableDir`）。
   - **安装入口也要校验 `dshMinVersion`**：列表已经过滤掉更早的版本，直接调
     `/api/dsh/versions/install` 也必须拒绝（装出来在子路径挂载下必然 404）。
   - **忙拒绝回 409**：`busyf(...)` 产生的错误经 `writeErrOperation` 映射成 409 Conflict，
     其余参数/状态错误仍是 400 —— 前端不依赖状态码，但「稍后重试就行」与「请求本身有问题」
     得区分开。

13. **面向用户的文案一律「后端给 code + 参数，前端查 i18n」** —— 用户可见的每一句话
   （toast / 弹窗 / 进度行 / 状态说明）都要能随界面语言切换，而语言只存在前端的
   localStorage 里，后端并不知道。机制在 `backend/uimsg.go`：
   - 后端只回**稳定 code + 参数**，中文原文作为「前端不认这个 code」时的兜底：
     `uiErr("err_need_version", "缺少 %s", "field", "version")`（错误）、
     `busyf("err_backup_running", "…")`（并发拒绝 → HTTP 409）、
     `writeErrU(w, 400, "err_need_version", "缺少 version")`（handler 内的文案）、
     `setMsgFields(&st.Message, &st.MessageRef, "msg_install_done", "dsh %s 安装完成", "version", v)`
     （状态里的进度/说明文案）。
   - code 就是前端 i18n 的 key（`frontend/src/composables/useI18n.ts` 的 zh / en 两份字典）。
     命名：错误 `err_*`、进度/状态 `msg_*`，小写 snake_case；参数名用 `detail` / `version` /
     `min` / `max` 这类，中文原文里用 `%s`（按参数顺序填），前端译文的占位符用 `{参数名}`。
     参数是「k/v 成对展开」的实参列表，不要传 map。
   - **只有面向用户的文案才带 code**：诊断类错误（会进日志、或只是 npm/exec 的原始输出）
     保持英文原文；状态里的诊断文本（npm 尾行、平台返回的原始 msg）用 `setMsgFields(…, "", line)`
     原样直出、**不带引用**（翻译原始输出没有意义）。
   - 前端渲染一律走两个 helper（别自己拼 `st.error || fallback`）：
     `uiText(text, ref, fallback)` 渲染「原文 + 引用」，`uiErrText(e, fallback)` 渲染抛出的
     错误（`serverapi` 的 `ApiError` 带 code/params）。`useI18n` 的 `t()` 只用于前端自己的文案。
   - 后端渲染的独立页面（登录页 / 等待页）用不了前端运行时，各自处理：登录页读
     `localStorage['console-language']` 用页内脚本换 `data-zh`/`data-en`（`backend/auth.go`）；
     等待页本身就是双语页，detail 给中英各一行（CSS `white-space:pre-line`）。
   - 守卫：`backend/uimsg_test.go` —— 用语法树扫出所有用户文案调用点，断言
     **每个 code 在前端 zh / en 两份字典里都有**、**占位符数量 == 参数个数**（防
     `%!s(MISSING)` 出现在界面上）、以及 setErrFields/setMsgFields 成对写入语义。
     新增一句提示 = 后端加 code + 前端补两条译文，跑一次 `go test` 就知道漏没漏。
   - 反面教材（本次踩过）：前端用 `st.error.includes('用户取消')` 判断「用户取消了下载」——
     语言一换就失效；现在后端在取消时置 `Cancelled` 标记 + code，前端只看结构化字段。

14. **控制台自更新只允许小版本，跨版本必须更新 fpk 安装包** —— harness 只能就地更新同一
   `major.minor` 线内的补丁版（`1.4.3 → 1.4.9`）；跨主要/次要版本（`1.4 → 1.5`、`1.x → 2.x`）
   一律**拒绝下载与安装**，改为提示用户更新 fpk 安装包。原因：平台侧的部署
   脚本/依赖与 dsh 的配套版本都在 fpk 里，控制台自己解开一个跨线的包只会得到「二进制换了、
   平台侧没换」的半升级状态 —— 因此**不要**给这条规则加任何「下载后自更新」的变体。
   - 判定只有一处：`update.go` 的 `harnessUpdateTarget` / `sameMinorLine`（基准是
     **仓库最新版**，不是「本线最高补丁版」—— 最新版一跨线，就地自更新整体停用，否则用户会
     在被新线取代的旧线上继续升级且永远看不到「需要换包」的提示）。结论写进状态的两个新字段：
     `StoreUpdate` / `StoreVersion`，与 `HasUpdate` **互斥**；`HasUpdate` 的语义因此被收紧为
     「有可就地下载安装的更新」，别再往里塞别的含义。
   - 三处闸门：`POST /api/update/download`（同步 400 + `err_update_store_required`，
     **不下字节、不改状态**）、`downloadUpdate`、`installUpdate`。新增任何「下载/安装 harness
     更新包」的入口，都要挂上同一判定。
   - 前端（`UpdateSection.vue`）：红点在 `hasUpdate || storeUpdate` 时亮（只看 `hasUpdate`
     会让跨线新版本完全静默）；弹窗给 `update_store_required` 文案、下载按钮整行隐藏、
     **更新内容照常显示**；「检查更新」不再误报「暂无更新」。回归测试见
     `backend/update_store_test.go`。

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
- **dsh 市场的「回环栅栏」要在反代上成对伪装，`isLoopbackFencedRoute` 里的两条路径
  缺一不可** —— dshmarket 既拦重启请求（`trustedRestartRequest`，POST `/dsh-market/restart`
  见转发标记头直接 403），又用状态轮询回答「从当前页面发起的重启能不能过栅栏」
  （`restartReachableFrom` → `GET /dsh-market/status` 的 `restartReachable` 字段，
  #782/#678），前端拿到 false 就**把「立即重启」按钮藏起来**。判据是「回环对端 +
  无 `Forwarded` / `x-forwarded-*` / `x-real-ip` + Host 是回环 authority」，所以反代
  `forward()` 对这两条路径都要删转发头、把 Host/Origin 改写成 dsh 上游。只伪装 restart
  会得到自相矛盾的状态（POST 能成功、按钮却不见）；只伪装 status 则 POST 吃 403。
  回归测试见 `backend/proxy_market_restart_test.go`。
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
- **`PROFILE_TEMPLATES.web.bundles` 注入已成历史** —— 旧流程在 `server-build.yaml` 的 CI 里
  把 `dshmarket` 塞进 server 包的 `dependencies` 与 profile 模板；现在 dsh 是控制台原样装的
  官方 npm 包，**不再有任何注入**，市场就是一个普通 profile 插件（装它 = `dsh plugin add`）。
  别再把「市场由 server 包提供」当成前提 —— 老设备上 profile 里可能还留着 `dshmarket` 在
  `dsh.profile.bundles` 里但没有依赖，那种状态在控制台里就是「未安装」，装上依赖即可。
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
     - `replaceBusyGuard()` 是所有「停 dsh 之前」的统一前置检查（先挡、再停），当前的调用点：
       **更新 harness 控制台**（`installHarness`）、**恢复 dsh 数据**（会删 `~/.dsh`，
       经 `stopDshForReplacement`）、**切换 dsh 版本**（`ServerManager.Switch`）、
       **市场安装/卸载后重启 dsh**（`restartDshForMarket`）。被拒绝时不产生任何停机、不改盘。
     - 守卫来源：市场 `/dsh-market/status` 的 busy + `DshManager.PluginCmdRunning()`；
       市场不回答视为不忙（恢复这类抢修不被挡）。**控制台自己发起的每一条插件命令都要
       登记进这个计数** —— 插件页的 list/remove 与插件市场的 add/remove/list 都必须走
       `DshManager.RunPluginCommand`（它顺带清陈旧锁）。曾经市场的 add 直接调
       `runDshCmdTimeout` 绕过它，于是忙守卫对「控制台正在装市场」完全瞎；回归测试见
       `backend/plugin_cmd_entry_test.go`（含一条源码守卫）。
     - 唯一例外是概览页的「停止/重启 dsh」按钮：那是用户的即时意图，**不挡**；改为在确认
       弹窗里提示 —— 弹窗打开时查 `GET /api/dsh/busy`（`AdminMux.dshBusySnapshot()`），
       有插件操作在跑就显示风险提示。这个端点刻意不塞进高频轮询的 `/api/dsh/status`。
  3. **「dsh 装在哪」只有两个入口**（`server.go` 顶部的两个函数）：`serverRootFor(renv)`
     给出 `${数据目录}/server`，`versionDirFor/versionBinDirFor/versionDshBinFor` 给出某个
     版本的目录与可执行文件。`DshManager`（PATH 注入、启动）与 `ServerManager`
     （列表/安装/删除/切换）都走它们，**不要再各拼一份路径**；测试靠 `renv.DataDir`
     指向临时目录，因此不会碰到真实的装盘目录。
- **主目录（dsh 的 HOME）不可切换，别再把这个功能加回来** —— 资源页曾经可以把某个
  「已授权目录」设为 dsh 的 HOME，现已整体移除（`AppConfig.HomeDir`、
  `/api/dsh/set-home`、`handleSetHome` 都没有了；`DshManager.effectiveHome()` 恒返回
  `renv.Home`）。原因不是「迁移配置不好使」，而是**目标目录的权限由平台共享模型决定**：
  飞牛用户共享目录用 `mode 000` + ACL + `system.trim_acl` 扩展属性授权，应用写出的
  每个文件能否被读，取决于飞牛层为它写下的那条逐文件记录；记录一旦写坏（实测见到
  24 字节的残缺记录，正常是 44/84 字节），**连文件属主（dsh 自己）读它都会 EACCES**，
  于是 `dsh plugin --profile web …` 在第一步读 `profiles/web/package.json` 时就抛
  未捕获异常退出（现象：插件列表空白、市场里装不上，控制台日志里什么都没有 ——
  因为根本没有陈旧锁可清，30 秒锁巡检再正确也无从下手）。在应用自己拥有（`owner=Harness`、
  mode 非 0）的目录里不会这样：即使同一条记录写坏，POSIX 属主权限仍然兜底。
  排查同类问题时：`cat <home>/.dsh/profiles/web/package.json` 应以 dsh 的身份（Harness）
  读得通，读不通就是这个问题。
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
  2. **键盘弹起时要连文档带 `main` 一起锁**：`html[data-kb]` 下终端页高度已是可视视口高度
     （`--vv-h`），但 App 根节点的 `min-h-screen`、`main` 的高度式与给键盘预留的
     `padding-bottom`（`--kb-inset`）仍可能让外层比可视视口高出一截，手指一拖照样把整页带走、
     辅助键栏随即离开键盘顶边。`style.css` 对 `html[data-kb]:has(.terminal-page)` 做两件事：
     把 html/body 的 `overflow` 锁成 `hidden`（外加 `overscroll-behavior: none` 断链式滚动），
     并把 `main` 退回 `height: auto`（移动端滚动在 `main` 上，见前面那条；用 h-auto 而不是
     overflow:hidden，否则 `--vv-h` 略大于内容盒的机型会把键栏裁掉）。`:has()` 限定范围很重要：
     终端页被 `KeepAlive` 切走后其 DOM 不在文档里、选择器自然不匹配，其它页面键盘弹起时
     **仍然要能滚动**到被键盘挡住的输入框。
     验证（无头 Chrome + CDP 触摸事件即可复现）：手机视口下沿终端上下拖动，`.xterm-rows` 文本要变化
     且 `window.scrollY` 恒为 0；注入 `data-kb` + `--vv-h` 后键栏底边必须正好落在 `--vv-h` 处、
     `main` 无滚动余量（`scrollHeight === clientHeight`），
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
- [ ] 新增的用户可见文案是否走「后端 code + 前端 i18n 两条译文」（见规则 13）？界面是用
      `uiText` / `uiErrText` 渲染的，而不是直接拼 `st.error`？