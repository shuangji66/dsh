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
  校验密码 → 起 Admin socket → 起 dsh（非 `HARNESS_AUTOSTART=0`）→ 换 Cookie →
  装 node-pty → 起反代 → 等信号退出。
- `config.go` — `AppConfig`（前端可改）与 `RuntimeEnv`（环境变量）。**反向代理端口
  （`ProxyPort`）只从环境变量读，不随配置保存。**
- `admin.go` — Admin mux（Unix socket）：`buildHandler()` 里一个大的 `switch` 分发
  所有 `/api/*` 路由；`spaHandler` 提供内嵌前端。
- `dsh.go` — `DshManager`：进程生命周期；`effectivePID`/`findDshPid` 处理**装插件自重启**
  后 PID 变化（扫 `/proc/<pid>/cmdline` 匹配 `dsh web ... --port <port>`）；
  `tokenScanner` 从 dsh 日志捕获 `?token=`；`ExchangeToken` 换 `dsh-auth-*` Cookie。
- `proxy.go` — 反向代理，转发时**携带 dsh 会话 Cookie**。
- `terminal.go` — WebSocket + `creack/pty` 的交互式 bash。
- `update.go` — 更新 harness / dsh 服务；含 GitHub 加速源回退数组 `updateAccelerators`；
  `harnessVersion` 由 `-ldflags -X` 注入。
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
- **`PROFILE_TEMPLATES.web.bundles` 注入** —— 只在 `server-build.yaml` 的 CI 中对
  `dsh-app-boot` 做，本地不涉及。
- **代理端口默认 `13079`、dsh 端口默认 `13080`** —— 冲突排查先看这两个。

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