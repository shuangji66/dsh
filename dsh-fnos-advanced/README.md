# dsh-fnos-advanced

为 DeepSeek Harness（dsh）的工作区目录选择器增加一个 **「选择飞牛目录」** 按钮：
点击后选择并授权飞牛（fnOS）目录，插件自动把授权路径填回 dsh 的目录选择器并按下
回车，选择器随即进入该目录。

- **在飞牛桌面 / 飞牛 App 内**：直接调用飞牛宿主的原生目录选择器，不打开网页授权。
- **在普通浏览器里**：通过飞牛授权页完成；如果同时配置了反代地址和内网地址，
  会先让用户选择用哪一个入口授权。
- **在 dsh 插件中心**：插件行提供设置页，可配置飞牛反代地址、内网地址/端口和默认入口。

- 不导入任何 `@deepseek-ai/*` 客户端/宿主插件包：目录选择器只依赖 dsh 的客户端
  bundle 装载约定与 `ctx.webServer.register` 服务；设置页使用 `plugins.row.config`
  slot 与共享 `react` 平台模块，slot 不存在时自动降级。
- 使用飞牛官方前端 SDK [`@trimjs/web-app`](https://www.npmjs.com/package/@trimjs/web-app)
  构造授权 URL、解析授权回调；SDK 已打进 `lib/client.js`，安装后无运行时依赖。
- 宿主侧只提供同源小路由：配置、授权回调、一次性结果暂存和设置；不保存任何凭据。

## 安装

### 通过插件管理器 / 市场

发布到 npm 后：

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
4. 选择目录并确认授权。
5. 插件自动把授权路径填入选择器的路径输入框并回车，选择器进入该目录。
6. 点选择器的「Open / 打开」即可把该目录添加为工作区。

> 目录授权完成后，飞牛会把该目录的 ACL 授予当前应用用户（`Harness`）；dsh 本身也以
> 同一用户运行，因此可以直接读写该目录。

## 插件中心设置页

在 dsh 的 **Plugins / 插件** 页打开 `dsh-fnos-advanced`，点「Configure / 配置」即可看到：

| 设置项 | 说明 |
| --- | --- |
| 反代飞牛地址 | 非常规反代下的飞牛外部地址，只填 `协议://域名[:端口]`，不要带路径；留空表示未配置 |
| 飞牛内网地址 / 端口 | 只填端口（默认 `5666`）时使用当前访问主机的地址；也可填完整地址，如 `http://192.168.1.10:5666` |
| 非网关访问时的授权入口 | `每次询问`（默认）、`优先使用反代地址`、`优先使用内网地址` |

设置保存在 `$DSH_HOME/fnos-advanced/settings.json`（`DSH_HOME` 默认 `~/.dsh`），
文件权限 `0600`。设置页使用 dsh 的 `plugins.row.config` slot；如果某个旧版本没有
该 slot，设置页不会显示，但目录选择器按钮仍可正常使用，并回退到自动推断的授权地址。

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
- 在底部按钮栏插入「选择飞牛目录」；如果找不到已知语言的按钮文案，会退化为
  「最后一个按钮的父节点」。
- 点击时同步 `window.open('', ...)` 预开授权窗口（避免异步后弹窗被浏览器拦截），再用
  官方 SDK 的 `buildAppAuthUrl('pickUserFile', …)` 构造授权地址并导航。
- 网页授权时，回调通过 `postMessage`、`localStorage` 与宿主侧
  `POST /fnos-advanced/auth-result` 三条通道回传；即使跨域跳转导致 `window.opener`
  被 COOP 切断、或 iframe 的 localStorage 被分区，dsh 页面仍能通过轮询宿主结果拿到
  授权路径。
- 非网关访问时先读取设置：反代地址和内网地址都存在且默认模式为「每次询问」时，
  在按钮上方显示入口选择面板；选择后再同步打开授权窗口。
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
| `GET /fnos-advanced/settings` | 读取插件中心设置 |
| `POST /fnos-advanced/settings` | 校验并保存设置到 `$DSH_HOME/fnos-advanced/settings.json` |
| `GET /fnos-advanced/health` | 健康检查 |

宿主侧不接触 token，也不代理文件内容；它只负责把授权结果安全地转交给同源的 dsh 页面。

## 跨 dsh 版本

- 客户端 bundle 只使用 dsh 的 `window.__ModuleLoader__.load({ id, factory })` 契约；
  factory 内是纯浏览器代码。
- 目录选择器不导入 `@deepseek-ai/dsh-client-ui-*`、不使用 slot、不读 CSS Module 类名。
- 设置页使用 dsh 的 `plugins.row.config` slot 和共享的 `react` 平台模块；slot 不存在
  时设置页自动缺席，不影响目录选择器。
- 宿主行只使用稳定的 `ctx.inject(['webServer'], …)` + `ctx.webServer.register({ kind, path, handler })`。
- 如果未来 dsh 改了目录选择器的 DOM 结构（标题、路径输入框、底部按钮），需要同步调整
  `src/client/index.js` 中的识别与注入逻辑。

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

## 发布（GitHub Release tarball）

仓库根目录的工作流：

```text
.github/workflows/dsh-fnos-advanced-release.yaml
```

仅手动触发：

- 先把 `dsh-fnos-advanced/package.json` 的 `version` 改成要发布的版本；
- Actions → **Release dsh-fnos-advanced** → Run workflow；
- 版本号由工作流直接读取 `package.json`，tag 自动生成为
  `dsh-fnos-advanced-v<version>`，不需要手动输入。

Release 产物：

```text
dsh-fnos-advanced-0.2.1.tgz
dsh-fnos-advanced-0.2.1.tgz.sha256
```

`.sha256` 是标准 `sha256sum` 输出，供控制台按「同名 + `.sha256`」规则取回校验。
Release 使用 `--latest=false`，不会影响本仓库 Harness 自身的 latest 更新检查。

从 Release 安装：

```bash
dsh plugin --profile web add \
  https://github.com/shuangji66/dsh/releases/download/dsh-fnos-advanced-v0.2.1/dsh-fnos-advanced-0.2.1.tgz
```

未来控制台做插件自我更新时，可直接按以下规则拼地址：

```text
tag      = dsh-fnos-advanced-v<version>
package  = https://github.com/<owner>/<repo>/releases/download/<tag>/dsh-fnos-advanced-<version>.tgz
checksum = <package>.sha256
```

## 已知限制

- 按钮文案固定为中文「选择飞牛目录」；如需英文/其他语言可自行修改源码。
- 网页授权需要浏览器允许弹窗；被拦截时按钮下方会提示。飞牛桌面/App 内的原生
  选择器不需要弹窗。
- 设置页依赖 dsh 的 `plugins.row.config` slot（0.1.6-alpha.2+ 风格）；旧版本没有该
  slot 时只能通过编辑 `$DSH_HOME/fnos-advanced/settings.json` 配置。
- 授权页由飞牛提供，系统/应用版本低于要求时无法完成授权。
- 回车后选择器会进入目标目录，仍需点一次「Open / 打开」才会真正添加工作区
  （这是 dsh 目录选择器本身的行为：路径回车 = 导航，Open = 确认）。
- 若部署在非常规反向代理下且自动探测失败，请用 `DSH_FNOS_AUTH_BASE` 或
  `localStorage` 覆盖授权地址。

## License

MIT
