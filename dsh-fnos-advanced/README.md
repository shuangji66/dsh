# dsh-fnos-advanced

为 DeepSeek Harness（dsh）增加一条 **飞牛（fnOS）文件通道**：

- 工作区目录选择器多一个 **「选择飞牛目录」** 按钮：授权后自动把路径填回 dsh 并进入该目录；
- 对话输入框的 **「+」命令菜单**（文件 / 目标 / 计划 …）多一项
  **「从飞牛选择」**：选中的文件直接进入对话草稿；**非图片是零拷贝的 `@路径` 引用**
  （不复制、不经过浏览器上传），图片按需读取字节以支持视觉；
- 右侧文件树的每一行（文件 / 目录）右侧多一个 **「⋯」操作菜单**：
  **在飞牛中打开**（仅飞牛桌面 / App）、**引用到对话**（仅文件）、**复制路径**；
- 工作区「更多」菜单与设置页提供 **目录权限体检 / 重新授权**，诊断飞牛 ACL 损坏或目录移动；
- 目录选择器支持 **新建文件夹**、**重复添加拦截**（只拦完全相同的目录，子目录仍可添加），并多一个 **共享目录** 入口；
- 设置页提供 **诊断**（版本、DOM 锚点、地址连通性）和 **界面语言**（跟随浏览器 / 中文 / English）；

- **在飞牛桌面 / 飞牛 App 内**：直接调用飞牛宿主的原生选择器，不打开网页授权；
  工作区的「更多」菜单还会多出 **「打开目录」**，调用飞牛文件管理器打开该工作区目录。
- **在普通浏览器里**：通过飞牛授权页完成；如果同时配置了反代地址和内网地址，
  会先让用户选择用哪一个入口授权。
- **在 dsh 插件中心**：插件行提供设置页，可配置飞牛反代地址、内网地址/端口、默认入口，
  以及是否允许读取图片内容。

- 不导入任何 `@deepseek-ai/*` 客户端/宿主插件包：目录选择器只依赖 dsh 的客户端
  bundle 装载约定与 `ctx.webServer.register` 服务；设置页使用 `plugins.bundle.config`
  slot 与共享 `react` 平台模块，slot 不存在时自动降级。
- 使用飞牛官方前端 SDK [`@trimjs/web-app`](https://www.npmjs.com/package/@trimjs/web-app)
  构造授权 URL、解析授权回调；SDK 已打进 `lib/client.js`，安装后无运行时依赖。
- 宿主侧只提供同源小路由：配置、授权回调、一次性结果暂存和设置；不保存任何凭据。

## 安装

### 通过插件管理器 / 市场

插件发布在 npm 官方 registry，用 dsh 的插件命令安装：

```bash
dsh plugin --profile web add dsh-fnos-advanced
```

安装后确认它已进入 profile 的组合包列表（插件管理器通常会自动处理）：

```json
{
  "dsh": {
    "profile": {
      "bundles": ["@deepseek-ai/dsh-base", "@deepseek-ai/dsh-web-app", "dsh-fnos-advanced"]
    }
  }
}
```

### 本地开发安装

```bash
# 在插件目录构建客户端 bundle
npm install
npm run build

# 用 dsh 插件命令安装本地目录
dsh plugin --profile web add /absolute/path/to/dsh-fnos-advanced
```

如果直接手工拷贝，需要同时：

1. 把包放到 `$DSH_HOME/profiles/web/node_modules/dsh-fnos-advanced`（或 `pnpm add` 一个
   `link:` 依赖）；
2. 把 `dsh-fnos-advanced` 加进 profile 的 `dsh.profile.bundles`；
3. 重启 dsh。

包内的 `cordis.patch.yml` 会插入宿主行；浏览器半侧由 `package.json` 的 `dsh.client`
声明自动发现。

## 使用

1. 在 dsh 侧边栏点「Add workspace / 添加工作区」。
2. 在目录选择器底部点击 **选择飞牛目录**。
3. 按当前环境走不同流程：
   - 飞牛桌面 / 飞牛 App 内：直接弹出飞牛原生目录选择器；
   - 通过飞牛网关访问：弹出同源飞牛授权页；
   - 普通浏览器且配置了反代 + 内网两个地址：先选择「通过反代地址」或
     「通过内网地址」，再打开对应授权页。
4. 选择目录并确认授权；如果点「取消」，插件提示「已取消选择目录」，不会显示
   飞牛宿主返回的 `Operation failed`。
5. 插件自动把授权路径填入选择器的路径输入框并回车，选择器进入该目录。
6. 点选择器的「Open / 打开」即可把该目录添加为工作区。

### 从飞牛选择文件进对话

1. 点对话输入框左侧的 **「+」** 打开命令菜单，点 **「从飞牛选择」**（在「文件」下面）。
2. 和目录一样：飞牛宿主内直接弹原生选择器；普通浏览器走网页授权。
3. 选中后：
   - **非图片**（文档、压缩包、视频、代码……）：dsh 里出现一个 `@/vol1/...` 引用芯片。
     文件不会被复制，也不会走浏览器上传；模型需要时按该路径读取原文件。
   - **图片**（PNG/JPEG/WebP/GIF）：插件通过宿主的一次性令牌接口读取字节，再交给 dsh
     的正常图片上传流程（视觉需要真实字节）；读取失败时自动降级为路径引用并提示。
4. 引用和普通附件一样随消息发送；移除芯片不会删除原文件。

### 文件树行操作

右侧文件树（Files）里，把鼠标移到文件或目录行上，行尾会出现 **「⋯」**：

| 动作 | 行为 |
| --- | --- |
| 在飞牛中打开 | 文件调用 `openFile` 用默认应用打开，目录调用 `openFileManager`；只在飞牛桌面 / App 内显示 |
| 引用到对话 | 把该文件作为 `@路径` 芯片插入当前对话草稿（图片会按设置读取字节上传；目录不提供此项） |
| 复制路径 | 复制绝对路径；HTTP 反代等无 Clipboard API 的环境用 `execCommand` 兜底 |

「引用到对话」复用「从飞牛选择」的同一条 composer 文件通道；菜单挂在 `body` 的覆盖层里，
不会被侧栏裁切，行高与层级结构不变，只给行尾按钮留出 34px 内边距。

### 目录选择器增强

- **新建文件夹**：原生飞牛选择器和网页授权都会带上 `creatable`；
- **重复添加拦截**：选中的路径与已有工作区**完全相等**时，提示「该目录已添加为工作区」，
  不填入选择器；比较前会去掉结尾 `/` 和重复斜杠。
- **子目录不受影响**：`/vol1/work` 已是工作区时，仍然可以把 `/vol1/work/project` 作为新工作区。
  这里**刻意不用 `disabledPaths`**：飞牛会把被禁路径的整个子树一起禁选，那正是之前无法添加
  子目录的原因。
- **共享目录**：目录选择器底部在「选择飞牛目录」旁多一个 **「共享」** 按钮（完整说明见 title），
  走 `pickSharedFile`（原生宿主）或 `/app-auth/pick-shared-file`（网页授权）。

网页授权对 `creatable` 是**尽力转发**（作为额外 query 参数）；旧版飞牛可能忽略，原生宿主
则走 SDK 的正式参数。重复添加拦截完全在客户端完成，不依赖选择器是否支持禁选。

### 工作区权限体检 / 重新授权

工作区「更多」菜单（桌面端悬停工作区行）新增：

- **检查目录权限**：宿主只读元数据（`stat` + `access`），返回目录是否存在、可读、可写；
- **重新授权目录**：飞牛桌面 / App 内直接调用 `authorizeUserFile(path)`；浏览器里走
  `/app-auth/authorize-user-file` 授权页，完成后自动复查权限。

设置页的 **工作区目录权限** 区域可以一次检查全部工作区，并逐行重新授权。检查过程不读取
目录内容，只接受工作区 id（不接受任意路径）。检查结果可以用 **清除结果** 收起，避免列表
一直占着设置页。

### 诊断

设置页的「连接与授权」「工作区目录权限」「诊断」三个大区块都可以点击标题折叠；诊断默认
折叠，展开后展示：

- 宿主 / 客户端版本、宿主接口状态、运行环境（飞牛桌面 / 网关 / 普通浏览器）、图片读取与
  工作区体检是否可用；
- 当前页面的 DOM 锚点：对话输入框、文件树（未打开文件页时显示「未检测」）；
- **测试反代地址 / 测试内网地址**：浏览器侧 `no-cors` 探测（4 秒超时）；
- **复制诊断信息**：把上述内容复制到剪贴板，方便反馈。

### 界面语言

设置页可选择 **跟随浏览器 / 中文 / English**，偏好保存在浏览器
`localStorage['dsh-fnos-advanced:language']`。客户端文案、toast、菜单、选择器标题和设置页
都会切换；飞牛授权回调页跟随浏览器语言。宿主返回的已知错误文案也会翻译，未知文案原样显示。

### 打开工作区目录（飞牛桌面 / App）

在飞牛桌面或飞牛 App 内，工作区行的「更多」菜单会增加「打开目录」：

- 点击后通过官方 SDK 的 `openFileManager(path)` 在飞牛文件管理器中打开该工作区目录；
- 独立浏览器 / 独立 Web App 环境不会显示该菜单项；
- 工作区路径优先从 dsh 的 `workspaces` 客户端服务读取，取不到时调用宿主路由
  `/fnos-advanced/workspace-path?id=<workspaceId>`（宿主侧优先查 `workspaceRegistry`，
  再回退读 `storages/workspace.json`）。

> 目录授权完成后，飞牛会把该目录的 ACL 授予当前应用用户（`Harness`）；dsh 本身也以
> 同一用户运行，因此可以直接读写该目录。

## 插件中心设置页

在 dsh 的 **Plugins / 插件** 页打开 `dsh-fnos-advanced` **包页面**，设置会直接显示在
「包含的组件」上方，不需要再点组件的 Configure 进入次级页面：

| 设置项 | 说明 |
| --- | --- |
| 反代飞牛地址 | 非常规反代下的飞牛外部地址，只填 `协议://域名[:端口]`，不要带路径；留空表示未配置 |
| 飞牛内网地址 / 端口 | 只填端口（默认 `5666`）时使用当前访问主机的地址；也可填完整地址，如 `http://192.168.1.10:5666` |
| 非网关访问时的授权入口 | `每次询问`（默认）、`优先使用反代地址`、`优先使用内网地址` |
| 从飞牛选择图片时读取图片内容 | 默认开启。关闭后图片也只生成路径引用（模型无法直接「看到」图片）；仅支持 PNG/JPEG/WebP/GIF，单个不超过 20 MB |

设置保存在 `$DSH_HOME/fnos-advanced/settings.json`（`DSH_HOME` 默认 `~/.dsh`），
文件权限 `0600`。设置页使用 dsh 的 `plugins.bundle.config` slot，注册 key 为包名
`dsh-fnos-advanced`；如果某个旧版本没有该 slot，设置页不会显示，但目录选择器按钮
仍可正常使用，并回退到自动推断的授权地址。

> **升级插件后需要重启 dsh。** 客户端 bundle 可能会热更新，但宿主半侧
> （`lib/index.js`）由 dsh 进程加载后不会自动替换。如果设置页提示“宿主半侧未提供
> 设置接口”或保存时出现 404/405，说明 dsh 仍在运行旧版宿主半侧；请在 Harness
> 控制台概览页重启 dsh，再刷新页面保存。设置页会显示宿主/客户端版本号，便于判断。

## 权限要求

- 飞牛系统版本 ≥ `1.2.0401`，飞牛 App 版本 ≥ `1.34.0`（`pickUserFile` 的要求）。
- 应用包 `config/resource` 需要声明：

  ```json
  {
    "api-scope": ["trim.file.userAccess"]
  }
  ```

  Harness 1.5.0 的 fpk 已包含该声明。若你从旧包升级，请确认这一项存在，否则授权页
  可能拒绝请求。

## 授权地址如何确定

插件按以下顺序确定飞牛授权页的 origin：

1. 宿主环境变量 `DSH_FNOS_AUTH_BASE`（推荐给自定义反向代理/端口部署）；
2. 浏览器 `localStorage['dsh-fnos-advanced:auth-base']`（临时覆盖，方便排查）；
3. 当前页面路径以 `/app/` 开头（经飞牛网关访问，如 `/app/Harness/dsh`）→ 同源；
4. HTTPS 标准端口 → 同源；
5. 其余情况 → 当前主机的 `:5666`（飞牛网关默认端口）；授权前会先探测当前 origin
   是否也提供 `/app-auth/`，若提供则优先同源。

常见部署：

| 访问方式 | 授权页 origin |
| --- | --- |
| `https://fnos.example/app/Harness/dsh` | `https://fnos.example` |
| `http://192.168.1.10:3079`（Harness 反代） | `http://192.168.1.10:5666` |
| `http://127.0.0.1:3099`（直接 dsh） | `http://127.0.0.1:5666` |

如需强制指定：

```js
// 浏览器控制台
localStorage.setItem('dsh-fnos-advanced:auth-base', 'https://fnos.example:5666');
```

或启动 dsh 时：

```bash
DSH_FNOS_AUTH_BASE=https://fnos.example:5666 dsh web
```

## 实现方式

### 浏览器半侧（`src/client/index.js`）

- 用 `MutationObserver` 观察 DOM，通过 `role="dialog"`、标题中的 workspace/工作区、
  路径输入框或「编辑路径」按钮、以及底部 Open/Cancel 按钮来识别目录选择器。
  **目录选择器本身不使用** dsh 的 CSS Module 类名或 slot 名称。
- 通过 `window.parent !== window` 判断是否在飞牛桌面/App 宿主内；是则调用官方 SDK
  的 `pickUserFile({ directory: true })`，直接使用宿主原生选择器；宿主桥不可用时
  自动回退到网页授权。
- 在底部按钮栏插入「选择飞牛目录」与「共享」；如果找不到已知语言的按钮文案，
  会退化为「最后一个按钮的父节点」。
- 点击时同步 `window.open('', ...)` 预开授权窗口（避免异步后弹窗被浏览器拦截），再用
  官方 SDK 的 `buildAppAuthUrl('pickUserFile', …)` 构造授权地址并导航。
- 网页授权时，回调通过 `postMessage`、`localStorage` 与宿主侧
  `POST /fnos-advanced/auth-result` 三条通道回传；即使跨域跳转导致 `window.opener`
  被 COOP 切断、或 iframe 的 localStorage 被分区，dsh 页面仍能通过轮询宿主结果拿到
  授权路径。
- 非网关访问时先读取设置：反代地址和内网地址都存在且默认模式为「每次询问」时，
  在按钮上方显示入口选择面板；选择后再同步打开授权窗口。
- 工作区「更多」菜单的「打开目录」通过 DOM 注入：监听 `data-row-key="workspace:…"`
  行上的点击，菜单出现后克隆现有菜单项样式并插入；仅在 `isStandaloneWeb === false`
  或 `sdk.isWeb === false` 的宿主环境启用。
- 「从飞牛选择」作为**命令菜单项**注入，而不是额外的工具栏按钮：菜单由
  `[data-trigger-menu]` + `role="listbox"` + `button[role="option"]` 组成，靠命令别名
  （`file` / `goal` / `plan`）识别，不依赖语言或 CSS Module 类名；识别后在「文件」项后面
  克隆一个同款菜单项（去掉复制过来的 active 态），描述样式借用邻近菜单项。
- 文件仍送进 `[data-conversation-region="composer"]` 的隐藏 `input[type=file][multiple]`；
  菜单是 portal，点击后从「当前可见的 composer」重新找 input，不依赖事件 target 的祖先链。
  找不到锚点时菜单项不出现，不影响原功能。
- 文件树行动作只用 `[data-files-entry][data-files-path]` 两个显式锚点（0.2.0-rc.2 与
  0.2.1-rc.2 都有）：`li` 里已有 `button` 行，操作按钮作为 `li` 的**兄弟节点**绝对定位
  在行尾，点击不会触发行本身的开/关；行按钮只加 `padding-right` 给按钮让位。
  菜单本身挂在 `body` 上（`position: fixed`），不会被侧栏的 `overflow` 裁切。
- 非图片引用依赖 dsh 的 `globalThis.__DSH_HOST_PATHS__` 桥：插件在**模块加载时**安装
  `pathFor(file)`（dsh 是在挂载 composer 时一次性读取这个全局对象，等到点击时再装就晚了），
  用 `WeakMap` 把 `File` 映射到真实路径；图片则先 `POST file-token` 再取字节。
- 用 `HTMLInputElement.prototype.value` 的原生 setter 写入路径，派发 `input` / `change`，
  再派发 `keydown/keypress/keyup` 的 Enter 事件；React 受控输入实测可用。
- 通过 `ctx.effect` 注册清理逻辑；插件卸载时移除按钮、样式与监听器。

### 宿主半侧（`lib/index.js`）

无任何 import，注册：

| 路径 | 作用 |
| --- | --- |
| `GET /fnos-advanced/config` | 返回 `appName`（来自 `TRIM_APPNAME`，默认 `Harness`）、`authBase` 覆盖值、版本 |
| `GET /fnos-advanced/auth-callback.html` | 解析飞牛授权回调 query，写 localStorage、`postMessage` 给 opener，并把原始 query POST 给宿主，然后关闭自己 |
| `POST /fnos-advanced/auth-result` | 回调页把 `{state, search}` 暂存到内存（5 分钟 TTL） |
| `GET /fnos-advanced/auth-result?state=…` | dsh 页面轮询并取走结果（一次性） |
| `GET /fnos-advanced/workspace-path?id=…` | 按 workspaceId 返回工作区路径（宿主 registry / 磁盘 registry 回退） |
| `GET /fnos-advanced/workspaces` | 列出全部工作区（id / path / title），供重复添加拦截与诊断使用 |
| `POST /fnos-advanced/workspace-health` | 只读检查工作区目录（`exists` / `readable` / `writable`）；只接受工作区 id |
| `GET /fnos-advanced/settings` | 读取插件中心设置 |
| `POST /fnos-advanced/settings` | 校验并保存设置到 `$DSH_HOME/fnos-advanced/settings.json` |
| `POST /fnos-advanced/file-token` | 校验图片路径并发放一次性读取令牌（60 秒、单次使用） |
| `GET /fnos-advanced/file-bytes?token=…` | 按令牌返回图片字节（仅图片白名单、≤20 MB、禁止 dsh 数据目录） |
| `GET /fnos-advanced/health` | 健康检查，含 `features.imageRead` |

宿主侧不接触飞牛登录 token。它只负责把授权结果转交给同源的 dsh 页面，以及为**用户刚选中的图片**
提供一次性的字节读取；没有通用文件读取路由：

- 只接受绝对路径 + 图片扩展名白名单（`.png/.jpg/.jpeg/.webp/.gif`）；
- 拒绝隐藏路径（任一段以 `.` 开头）与 `DSH_HOME` 内的文件；
- 普通文件、空文件、非图片、超过 20 MB 一律拒绝；
- 令牌 60 秒过期、单次使用；发放前按真实路径（`realpath`）校验，读取时用已打开的文件描述符；
- 仅接受同源请求（`Origin`/`Referer` 与 `Host`/`X-Forwarded-Host` 匹配），跨站请求直接 403；
- 设置页可整体关闭（`imageRead: false`）。

> dsh 的 webServer 本身没有服务端鉴权，通常由外层控制台/网关挡在前面。因此这里刻意不做
> 「`/file?path=`」这类通用读取：插件路由一旦暴露到网络，通用读文件就等于把整台 NAS 交出去。

## 跨 dsh 版本

- 客户端 bundle 只使用 dsh 的 `window.__ModuleLoader__.load({ id, factory })` 契约；
  factory 内是纯浏览器代码。
- 目录选择器不导入 `@deepseek-ai/dsh-client-ui-*`、不使用 slot、不读 CSS Module 类名。
- 设置页使用 dsh 的 `plugins.bundle.config` slot 和共享的 `react` 平台模块，挂在插件
  包页面而非组件行页面；slot 不存在时设置页自动缺席，不影响目录选择器。
- 宿主行只使用稳定的 `ctx.inject(['webServer'], …)` + `ctx.webServer.register({ kind, path, handler })`。
- 如果未来 dsh 改了目录选择器的 DOM 结构（标题、路径输入框、底部按钮），需要同步调整
  `src/client/index.js` 中的识别与注入逻辑。
- 对话文件通道依赖两个已被 0.2.0-rc.2 / 0.2.1-rc.2 验证的稳定面：composer 的
  `data-conversation-region="composer"` + 隐藏 file input，以及
  `__DSH_HOST_PATHS__.pathFor(file)`。命令菜单锚点缺失时菜单项不出现；图片读取接口缺失
  （旧宿主）时图片降级为路径引用；两种情况都不会影响 dsh 原有的附件上传按钮。

## 开发与构建

```bash
npm install
npm run build      # 生成 lib/client.js（含 @trimjs/web-app）
npm pack           # 查看发布内容
```

构建脚本会把 `@trimjs/web-app` 与客户端源码一起打成 dsh 需要的惰性 CJS factory；
构建后会检查 bundle 中不再残留对 `@trimjs/web-app` 的运行时 `require`。

目录结构：

```
dsh-fnos-advanced/
├── cordis.patch.yml       # 插入宿主行
├── lib/
│   ├── index.js           # 宿主：路由与回调页
│   └── client.js          # 构建产物：浏览器 bundle
├── src/client/index.js    # 浏览器源码
├── scripts/build.mjs      # esbuild 构建脚本
└── package.json
```

## 发布（npm）

发布到 npm 官方 registry，工作流在仓库根目录：

```text
.github/workflows/dsh-fnos-advanced-publish.yaml
```

仅手动触发：

- 先把 `dsh-fnos-advanced/package.json` 的 `version` 改成要发布的版本
  （npm 不允许覆盖已发布的版本，版本已存在时工作流会直接失败）；
- Actions → **Publish dsh-fnos-advanced to npm** → Run workflow；
- 版本号由工作流读取 `package.json`，不需要手动输入。

认证用仓库 Secret `NPM`：该 token 属于 npmjs.com 账号 **shuangji66**，需要
具备发布权限并能跳过 2FA（classic 的 Automation / Publish token，或对该包有
read+write 权限且勾选 bypass 2FA 的 granular token）。工作流会先跑 `npm whoami`，
账号不是 `shuangji66` 就失败，避免用别的账号发出去。

工作流顺序：`npm ci` → `npm test` → `npm whoami` 校验 → 版本占用检查 →
`npm publish`（`prepack` 会自动重新构建 `lib/client.js`）。

发布后：

```text
包页面：https://www.npmjs.com/package/dsh-fnos-advanced
安装：  dsh plugin --profile web add dsh-fnos-advanced
```

查询已发布的最新版本：

```bash
npm view dsh-fnos-advanced version
```

插件的更新检测同样以 registry 为准（`npm view dsh-fnos-advanced versions`），
不再依赖 GitHub Release 的 tarball 与 `.sha256`。

## 已知限制

- 界面语言目前只影响插件自己的 UI；dsh 本体语言由 dsh 设置决定。
- 宿主返回的未知错误文案不翻译（保留原文，便于排查）。
- 「从飞牛选择」是鼠标/触摸可点的菜单项；dsh 的上下键高亮由它自己的 React 列表维护，
  不会把注入项算进键盘导航。
- 文件树行尾的「⋯」在桌面端悬停显示，触屏设备常显；目录不提供「引用到对话」
  （dsh 的 composer 文件 input 无法表达目录引用）。「在飞牛中打开」只在飞牛宿主内出现。
- 「从飞牛选择」一次能选几个文件由飞牛宿主的选择器决定（插件接受返回的全部路径）。
- 图片以外的文件**不会**被插件读取内容，只生成路径引用；如果模型的文件工具无权访问该路径，
  需要先在工作区/权限里授权（飞牛选择器已经为你选中的路径授权）。
- 网页授权需要浏览器允许弹窗；被拦截时按钮下方会提示。飞牛桌面/App 内的原生
  选择器不需要弹窗。
- 设置页依赖 dsh 的 `plugins.bundle.config` slot；旧版本没有该 slot 时只能通过编辑
  `$DSH_HOME/fnos-advanced/settings.json` 配置。
- 「打开目录」只在飞牛桌面 / 飞牛 App 宿主内显示；独立浏览器/独立 Web App 不显示。
  若宿主 SDK 不提供 `openFileManager`，点击后会提示打开失败。
- 授权页由飞牛提供，系统/应用版本低于要求时无法完成授权。
- 回车后选择器会进入目标目录，仍需点一次「Open / 打开」才会真正添加工作区
  （这是 dsh 目录选择器本身的行为：路径回车 = 导航，Open = 确认）。
- 若部署在非常规反向代理下且自动探测失败，请用 `DSH_FNOS_AUTH_BASE` 或
  `localStorage` 覆盖授权地址。

## License

MIT
