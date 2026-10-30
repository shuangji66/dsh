(function () {
  if (!window.__ModuleLoader__ || typeof window.__ModuleLoader__.load !== "function") {
    console.warn("[dsh-fnos-advanced] client module loader is not available");
    return;
  }
  window.__ModuleLoader__.load({
  id: "dsh-fnos-advanced",
  factory: (require) => {
    var module = { exports: {} };
    var exports = module.exports;
"use strict";
var __create = Object.create;
var __defProp = Object.defineProperty;
var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __getProtoOf = Object.getPrototypeOf;
var __hasOwnProp = Object.prototype.hasOwnProperty;
var __export = (target, all) => {
  for (var name in all)
    __defProp(target, name, { get: all[name], enumerable: true });
};
var __copyProps = (to, from, except, desc) => {
  if (from && typeof from === "object" || typeof from === "function") {
    for (let key of __getOwnPropNames(from))
      if (!__hasOwnProp.call(to, key) && key !== except)
        __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
  }
  return to;
};
var __toESM = (mod, isNodeMode, target) => (target = mod != null ? __create(__getProtoOf(mod)) : {}, __copyProps(
  // If the importer is in node compatibility mode or this is not an ESM
  // file that has been converted to a CommonJS file using a Babel-
  // compatible transform (i.e. "__esModule" has not been set), then set
  // "default" to the CommonJS "module.exports" for node compatibility.
  isNodeMode || !mod || !mod.__esModule ? __defProp(target, "default", { value: mod, enumerable: true }) : target,
  mod
));
var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);

// src/client/index.ts
var index_exports = {};
__export(index_exports, {
  apply: () => apply,
  install: () => install
});
module.exports = __toCommonJS(index_exports);

// src/client/anchor-status.ts
var observations = {
  composer: null,
  fileTree: null
};
function pickComposerObservation(regions, at) {
  if (!regions.length) return null;
  return { at, found: regions.some((region) => region.hasMultiFileInput) };
}
function pickFileTreeObservation(trees, at) {
  if (!trees.length) return null;
  const rows = trees.reduce((total, tree) => total + tree.rows, 0);
  if (rows > 0) return { at, found: true, count: rows };
  const rowLike = trees.reduce((total, tree) => total + tree.rowLike, 0);
  return { at, found: false, empty: rowLike === 0 };
}
function classifyAnchor(observation) {
  if (!observation) return "never";
  if (observation.found) return "found";
  return observation.empty ? "empty" : "missing";
}
function rememberComposerObservation(observation) {
  if (observation) observations.composer = observation;
}
function rememberFileTreeObservation(observation) {
  if (observation) observations.fileTree = observation;
}
function anchorObservation(name) {
  return observations[name];
}
var RECORD_MIN_INTERVAL_MS = 750;
var lastRecordAt = Number.NEGATIVE_INFINITY;
function recordAnchorObservations(now = Date.now()) {
  if (now - lastRecordAt < RECORD_MIN_INTERVAL_MS) return;
  lastRecordAt = now;
  const at = now;
  rememberComposerObservation(pickComposerObservation(
    Array.from(document.querySelectorAll('[data-conversation-region="composer"]'), (region) => ({
      hasMultiFileInput: Boolean(region.querySelector('input[type="file"][multiple]'))
    })),
    at
  ));
  rememberFileTreeObservation(pickFileTreeObservation(
    Array.from(document.querySelectorAll("[data-files-root]"), (root) => ({
      rows: root.querySelectorAll("[data-files-entry][data-files-path]").length,
      // Row-shaped elements without the anchors mean the markup changed. `li` is
      // dsh's row element in every version this plugin has shipped against, but
      // `[data-files-entry]` alone already proves "rows exist" even if the tag
      // changes — without it a renamed element was reported as an empty folder.
      rowLike: root.querySelectorAll('[data-files-entry], li, [role="treeitem"]').length
    })),
    at
  ));
}

// src/client/cancel.ts
var CANCEL_TEXTS = /* @__PURE__ */ new Set([
  "operation failed",
  "操作失败",
  "cancel",
  "canceled",
  "cancelled",
  "abort",
  "aborted",
  "取消",
  "已取消",
  "用户取消"
]);
function isCancellationText(text) {
  const value = String(text ?? "").trim().toLowerCase();
  if (!value) return false;
  return CANCEL_TEXTS.has(value) || value.includes("取消");
}
function isPickerCancelled(result) {
  if (!result) return true;
  const data = Array.isArray(result.data) ? result.data : [];
  if (data.length > 0) return false;
  if (result.code === 0) return true;
  const msg = String(result.msg || "").trim();
  if (!msg) return result.code === 1;
  return isCancellationText(msg);
}
var CANCEL_STATUSES = /* @__PURE__ */ new Set(["cancel", "cancelled", "canceled", "abort", "aborted"]);
var CANCEL_ERRORS = /* @__PURE__ */ new Set(["access_denied", "user_cancelled", "user_canceled", "user_cancel"]);
function isAuthCancelled(auth) {
  if (!auth) return false;
  const status = String(auth.status ?? "").trim().toLowerCase();
  const error = String(auth.error ?? "").trim().toLowerCase();
  if (status === "success") return false;
  if (CANCEL_STATUSES.has(status)) return true;
  if (CANCEL_ERRORS.has(error)) return true;
  return isCancellationText(error);
}

// src/client/constants.ts
var LOG_PREFIX = "[dsh-fnos-advanced]";
var MSG_TYPE = "dsh-fnos-advanced:auth";
var STORAGE_KEY = "dsh-fnos-advanced:auth-result";
var AUTH_BASE_KEY = "dsh-fnos-advanced:auth-base";
var SETTINGS_SLOT_KEY = "dsh-fnos-advanced";
var BUTTON_ATTR = "data-dsh-fnos-advanced";
var PICK_ITEM_ATTR = "data-dsh-fnos-pick-file";
var REF_PICK_ITEM_ATTR = "data-dsh-fnos-ref-pick";
var FILE_ACTION_ATTR = "data-dsh-fnos-file-action";
var SLOT_ACTION_ATTR = "data-dsh-fnos-slot-action";
var OPEN_DIR_ATTR = "data-dsh-fnos-open-dir";
var CHECK_DIR_ATTR = "data-dsh-fnos-check-dir";
var REAUTH_DIR_ATTR = "data-dsh-fnos-reauth-dir";
var STATUS_CLASS = "dsh-fnos-advanced-status";
var STYLE_ID = "dsh-fnos-advanced-style";
var POPUP_FEATURES = "popup=yes,width=760,height=680,resizable=yes,scrollbars=yes";
var AUTH_TIMEOUT_MS = 5 * 60 * 1e3;
var WORKSPACE_TITLE_RE = /workspace|工作区/i;
var PATH_LABEL_RE = /(?:^|\b)(?:edit\s*path|path)(?:\b|$)|路径|编辑路径|修改路径/i;
var OPEN_BUTTON_RE = /^(?:open|打开|确定|选择|select|choose)/i;
var CANCEL_BUTTON_RE = /^(?:cancel|取消)/i;
var FOLDER_OPEN_SVG = '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" stroke-width="1"><path d="M1.5 3.5C1.5 2.95 1.95 2.5 2.5 2.5H5.4C5.65 2.5 5.88 2.59 6.07 2.75L7.7 4.2C7.88 4.36 8.12 4.45 8.36 4.45H13.5C14.05 4.45 14.5 4.9 14.5 5.45V12.5C14.5 13.05 14.05 13.5 13.5 13.5H2.5C1.95 13.5 1.5 13.05 1.5 12.5V3.5Z" stroke="currentColor"/></svg>';
var PICK_FILE_SVG = '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path d="M9.5 1.5H4.5C3.95 1.5 3.5 1.95 3.5 2.5V13.5C3.5 14.05 3.95 14.5 4.5 14.5H11.5C12.05 14.5 12.5 14.05 12.5 13.5V4.5L9.5 1.5Z" stroke="currentColor" stroke-linejoin="round"/><path d="M9.5 1.5V4.5H12.5" stroke="currentColor" stroke-linejoin="round"/><path d="M6 8.5H10M8 6.5V10.5" stroke="currentColor" stroke-linecap="round"/></svg>';
var FILE_ACTION_SVG = '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><circle cx="3.5" cy="8" r="1.15" fill="currentColor"/><circle cx="8" cy="8" r="1.15" fill="currentColor"/><circle cx="12.5" cy="8" r="1.15" fill="currentColor"/></svg>';

// src/client/i18n.ts
var PREFERENCE_KEY = "dsh-fnos-advanced:language";
var EN = Object.freeze({
  // Common
  "取消": "Cancel",
  "保存": "Save",
  "保存中…": "Saving…",
  "已保存": "Saved",
  "读取设置…": "Loading settings…",
  "未设置": "Not set",
  "重试": "Retry",
  // Picker
  "选择飞牛目录": "Choose fnOS folder",
  "通过飞牛授权选择一个目录（会授予当前飞牛应用访问权限）": "Choose a folder through fnOS (grants the current app access)",
  "选择工作区目录": "Choose workspace folder",
  "确认授权": "Authorize",
  "选择文件": "Choose files",
  "添加引用": "Add reference",
  "飞牛授权": "fnOS authorization",
  "正在打开飞牛授权页面…": "Opening the fnOS authorization page…",
  "准备授权…": "Preparing…",
  "等待选择…": "Waiting for selection…",
  "等待授权…": "Waiting for authorization…",
  "选择授权入口…": "Choosing authorization entry…",
  "已取消选择": "Selection cancelled",
  "已取消授权": "Authorization cancelled",
  "已填入授权目录：{path}": "Folder authorized: {path}",
  "已填入路径": "Path filled",
  "最近使用": "Recent",
  "填入最近使用的目录：{path}": "Fill the recently used folder: {path}",
  "清除最近记录": "Clear recent folders",
  "该目录已添加为工作区": "This folder is already a workspace",
  "打开选择器失败": "Could not open the picker",
  "当前宿主未提供内嵌选择器，已改用网页授权。": "The native picker is unavailable; using web authorization.",
  "浏览器拦截了授权窗口，请允许弹窗后重试。": "The browser blocked the authorization window. Allow popups and retry.",
  "无法打开授权页面，请重试。": "Could not open the authorization page. Try again.",
  "授权超时，请重试": "Authorization timed out. Retry.",
  "授权窗口已关闭": "Authorization window closed",
  "授权结果里没有路径": "Authorization returned no path",
  "选择授权入口": "Choose authorization entry",
  "通过反代地址": "Via reverse proxy",
  "通过内网地址": "Via LAN",
  "没有找到目录选择器的路径输入框": "Could not find the folder picker's path input",
  "无法确定工作区": "Could not determine the workspace",
  "无法获取工作区目录": "Could not read the workspace folder",
  "打开目录失败，当前宿主可能不支持该操作。": "Could not open the folder; the host may not support it.",
  "打开目录": "Open folder",
  // Composer
  "从飞牛选择": "Choose from fnOS",
  "选择 NAS 上的文件，非图片作为路径引用": "Pick files on the NAS; non-images become path references",
  "NAS 文件": "NAS file",
  "没有找到对话的文件输入框，当前 dsh 版本可能不支持。": "No composer file input found; this dsh version may be unsupported.",
  "当前会话暂时不能添加附件。": "This session cannot accept attachments right now.",
  "已添加 {count} 个文件": "Added {count} file(s)",
  "已添加 {count} 个文件（{images} 张图片会上传）": "Added {count} file(s) ({images} image(s) will upload)",
  "{count} 张图片未能读取，已改为路径引用": "{count} image(s) could not be read; added as path references",
  "{count} 张图片读取失败（可能超过 8 MB），已改为路径引用": "{count} image(s) could not be read (possibly over 8 MB); added as path references",
  "{count} 张图片格式不支持视觉（如 HEIC），已改为路径引用": "{count} image(s) use a format vision does not support (such as HEIC); added as path references",
  "{count} 张图片未读取（设置已关闭图片读取），已改为路径引用": "{count} image(s) were not read because image reading is off; added as path references",
  "没有可添加的文件": "No files to add",
  "宿主半侧未提供图片读取接口": "The host does not provide the image read route",
  "读取图片失败（HTTP {status}）": "Could not read the image (HTTP {status})",
  "已复制路径": "Path copied",
  "复制失败，请手动复制": "Copy failed; copy manually",
  // File actions
  "飞牛操作": "fnOS actions",
  "在飞牛中打开": "Open in fnOS",
  "文件详情": "File details",
  "引用到对话": "Quote in chat",
  "复制路径": "Copy path",
  "在飞牛中打开失败，当前宿主可能不支持该操作。": "Could not open in fnOS; the host may not support it.",
  "无法显示文件详情，当前宿主可能不支持该操作。": "Could not show file details; the host may not support it.",
  "无法确定文件路径": "Could not determine the file path",
  "没有找到对话输入框，无法引用。": "No composer input to quote into.",
  // Workspace health
  "检查目录权限": "Check folder access",
  "重新授权目录": "Re-authorize folder",
  "正在检查目录权限…": "Checking folder access…",
  "目录可读写": "Folder is readable and writable",
  "目录不存在": "Folder does not exist",
  "不是目录": "Not a folder",
  "目录可读但不可写": "Folder is readable but not writable",
  "目录不可读（{error}）": "Folder is not readable ({error})",
  "检查失败：{error}": "Check failed: {error}",
  "重新授权目录：{path}": "Re-authorize folder: {path}",
  "已重新授权，请复查权限": "Re-authorized; check access again",
  "重新授权失败：{error}": "Re-authorization failed: {error}",
  "工作区目录权限": "Workspace folder access",
  "检查 dsh 工作区目录的读写权限。飞牛 ACL 记录损坏或目录移动后，dsh 会读不到这些目录。": "Check read/write access to dsh workspace folders. Broken fnOS ACL records or moved folders make dsh unable to read them.",
  "检查全部工作区": "Check all workspaces",
  "检查中…": "Checking…",
  "共 {count} 个": "{count} total",
  "可读写": "Read/write",
  "不存在": "Missing",
  "只读": "Read-only",
  "不可读": "No access",
  "检查失败": "Check failed",
  "重新授权": "Re-authorize",
  "已重新授权": "Re-authorized",
  "没有找到工作区": "No workspaces found",
  "没有找到工作区权限接口": "The workspace access API is not available",
  "权限体检": "Access check",
  "工作区体检": "Workspace access check",
  // Diagnostics
  "诊断": "Diagnostics",
  "检查 dsh 版本、DOM 锚点和飞牛地址连通性。": "Check dsh versions, DOM anchors, and fnOS address reachability.",
  "版本": "Version",
  "宿主": "Host",
  "客户端": "Client",
  "宿主接口": "Host API",
  "正常": "OK",
  "缺失（需重启 dsh）": "Missing (restart dsh)",
  "读取失败": "Error",
  "运行环境": "Runtime",
  "飞牛桌面 / App": "fnOS desktop / app",
  "飞牛桌面": "fnOS desktop",
  "飞牛 App": "fnOS app",
  "飞牛网关": "fnOS gateway",
  "普通浏览器": "Browser",
  "DOM 锚点": "DOM anchors",
  "对话输入框": "Composer input",
  "文件树": "File tree",
  "工作区菜单": "Workspace menu",
  "目录选择器": "Folder picker",
  "可用": "Present",
  "不可用": "Missing",
  "未检测": "Not detected",
  "可用（{time}）": "Present ({time})",
  "可用（{time} · {count} 行）": "Present ({time} · {count} rows)",
  "不可用（{time}）": "Missing ({time})",
  "未检测（文件树为空）": "Not detected (file tree is empty)",
  "对话输入框与文件树只在对话页存在，这里显示最近一次在对应页面的检测结果。": "The composer and the file tree only exist on the conversation page; this is the last check made there.",
  "测试反代地址": "Test proxy",
  "测试内网地址": "Test LAN",
  "测试中…": "Testing…",
  "可连接（{ms}ms）": "Reachable ({ms}ms)",
  "无法连接": "Unreachable",
  "连接超时": "Timed out",
  "复制诊断信息": "Copy diagnostics",
  "重新检测": "Re-check",
  "诊断信息已复制": "Diagnostics copied",
  "复制失败": "Copy failed",
  "图片读取": "Image read",
  "开": "On",
  "关": "Off",
  // Settings
  "飞牛目录授权": "fnOS access",
  "配置非网关访问时使用的飞牛授权入口；设置保存在当前 dsh 数据目录。": "Configure the fnOS authorization entry used outside the gateway. Settings are stored in the dsh data directory.",
  "反代飞牛地址": "fnOS reverse-proxy address",
  "填协议 + 域名[:端口]，不要带路径；留空表示未配置。": "Protocol + host[:port], no path; leave empty if unused.",
  "飞牛内网地址 / 端口": "fnOS LAN address / port",
  "只填端口时使用当前访问主机的地址；默认 5666。": "A bare port uses the current host; default 5666.",
  "非网关访问时的授权入口": "Authorization entry outside the gateway",
  "每次询问（推荐）": "Ask every time (recommended)",
  "优先使用反代地址": "Prefer reverse proxy",
  "优先使用内网地址": "Prefer LAN",
  "从飞牛选择图片时读取图片内容": "Read image content when picking from fnOS",
  "图片需要字节才能被模型「看到」；关闭后图片也只生成路径引用。仅支持 PNG/JPEG/WebP/GIF，宿主只读取你选中的文件。": "Images need bytes to be visible to the model. When off, images become path references. Only PNG/JPEG/WebP/GIF are read, and only for files you picked.",
  "连接与授权": "Connection and authorization",
  "配置反代/内网授权入口、界面语言和图片读取。": "Configure proxy/LAN authorization, interface language, and image reading.",
  "清除结果": "Clear results",
  "展开": "Expand",
  "收起": "Collapse",
  "界面语言": "Interface language",
  "跟随系统": "Follow system",
  "中文": "Chinese",
  "English": "English",
  "宿主半侧版本 {host}，客户端 {client}。请重启 dsh 后再保存设置。": "Host {host}, client {client}. Restart dsh before saving.",
  "宿主半侧未提供设置接口，通常是插件升级后没有重启 dsh。请重启 dsh 后再保存设置。": "The host half does not expose the settings API (usually after an upgrade without restarting dsh). Restart dsh and try again.",
  "无法读取宿主设置接口，请确认 dsh 运行正常。": "Could not read the host settings API. Check that dsh is running.",
  "保存失败（HTTP {status}）": "Save failed (HTTP {status})",
  "当前运行在飞牛桌面/宿主内：会直接调用飞牛目录选择器，不打开网页授权。": "Running inside fnOS desktop/app: the native picker is used; no web authorization popup.",
  "当前运行在飞牛 App 内：目录授权打开平台的网页授权页，文件详情/在飞牛中打开仍走宿主桥接。": "Running inside the fnOS app: folder authorization opens the platform web page; file details / open-in-fnOS still use the host bridge.",
  "当前通过飞牛网关访问：直接使用当前同源地址授权，不会弹出入口选择。": "Accessed through the fnOS gateway: the same origin is used for authorization; no entry chooser.",
  "当前不是通过飞牛网关访问：同时配置反代与内网地址后，点击「选择飞牛目录」会让你选择授权入口。": "Not accessed through the fnOS gateway: with both proxy and LAN configured, choosing a folder asks which entry to use.",
  "反代 {proxy} · 内网 {lan}": "Proxy {proxy} · LAN {lan}",
  // Host errors (surface through settings / pickers)
  "反代地址不是有效 URL": "The proxy address is not a valid URL",
  "反代地址只支持 http/https": "The proxy address must be http or https",
  "内网端口必须在 1-65535 之间": "The LAN port must be between 1 and 65535",
  "内网地址格式无效": "The LAN address is invalid",
  "内网地址只支持 http/https": "The LAN address must be http or https",
  "图片路径无效": "Invalid image path",
  "图片路径必须是绝对路径": "The image path must be absolute",
  "不允许读取隐藏路径": "Hidden paths cannot be read",
  "图片不存在": "The image does not exist",
  "不允许读取 dsh 数据目录": "The dsh data directory cannot be read",
  "只支持 PNG/JPEG/WebP/GIF 图片": "Only PNG/JPEG/WebP/GIF images are supported",
  "不是普通文件": "Not a regular file",
  "图片内容为空": "The image is empty",
  "图片超过 8 MB": "The image exceeds 8 MB",
  "插件设置已关闭图片读取": "Image reading is disabled in the plugin settings",
  "设置格式无效": "Invalid settings format"
});
var preferences = ["system", "zh", "en"];
var preference = "system";
var loaded = false;
var hostLanguage = null;
var hostLanguageResolver = null;
var hostLanguageResolving = null;
var languageListeners = /* @__PURE__ */ new Set();
function isPreference(value) {
  return typeof value === "string" && preferences.includes(value);
}
function normalizePreference(value) {
  if (isPreference(value)) return value;
  if (value === "auto" || value === "host") return "system";
  return null;
}
function normalizeHostLanguage(value) {
  const text = typeof value === "string" ? value : "";
  if (/^zh/i.test(text)) return "zh";
  if (/^en/i.test(text)) return "en";
  return null;
}
function setHostLanguageResolver(resolver) {
  hostLanguageResolver = resolver;
}
function notifyLanguageChange() {
  for (const listener of [...languageListeners]) {
    try {
      listener();
    } catch {
    }
  }
}
function onLanguageChange(listener) {
  languageListeners.add(listener);
  return () => {
    languageListeners.delete(listener);
  };
}
function resolveHostLanguage(force = false) {
  if (!force && hostLanguage) return Promise.resolve(hostLanguage);
  if (!force && hostLanguageResolving) return hostLanguageResolving;
  if (!hostLanguageResolver) return Promise.resolve(hostLanguage);
  const before = currentLanguage();
  hostLanguageResolving = (async () => {
    try {
      const config = await hostLanguageResolver?.();
      const next = normalizeHostLanguage(config?.language);
      if (next) hostLanguage = next;
    } catch {
    }
    hostLanguageResolving = null;
    if (currentLanguage() !== before) notifyLanguageChange();
    return hostLanguage;
  })();
  return hostLanguageResolving;
}
function getLanguagePreference() {
  if (loaded) return preference;
  loaded = true;
  try {
    const stored = normalizePreference(window.localStorage.getItem(PREFERENCE_KEY));
    if (stored) preference = stored;
  } catch {
  }
  return preference;
}
function setLanguagePreference(value) {
  const before = currentLanguage();
  preference = normalizePreference(value) || "system";
  loaded = true;
  try {
    window.localStorage.setItem(PREFERENCE_KEY, preference);
  } catch {
  }
  if (preference === "system") void resolveHostLanguage();
  if (currentLanguage() !== before) notifyLanguageChange();
  return preference;
}
function currentLanguage() {
  getLanguagePreference();
  if (preference === "zh" || preference === "en") return preference;
  if (hostLanguage) return hostLanguage;
  const html = typeof document !== "undefined" ? document.documentElement.lang : "";
  const navigatorLanguage = typeof navigator !== "undefined" ? navigator.language : "";
  return /^zh/i.test(html || navigatorLanguage) ? "zh" : "en";
}
function interpolate(text, params) {
  if (!params) return text;
  return text.replace(/\{(\w+)\}/g, (match, key) => Object.prototype.hasOwnProperty.call(params, key) ? String(params[key]) : match);
}
function lookup(source) {
  if (currentLanguage() !== "en") return source;
  const dictionary = EN;
  return dictionary[source] || source;
}
function t(source, params) {
  return interpolate(lookup(source), params);
}
function translateError(error) {
  const text = error == null ? "" : String(error);
  return text ? lookup(text) : text;
}

// src/client/dom.ts
function isVisible(element) {
  if (!element) return false;
  const node = element;
  return Boolean(node.offsetWidth || node.offsetHeight || node.getClientRects().length);
}
function sleep(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}
function randomId() {
  try {
    if (window.crypto && typeof window.crypto.randomUUID === "function") {
      return window.crypto.randomUUID();
    }
  } catch {
  }
  return `fnos-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}
async function copyText(text) {
  try {
    if (navigator.clipboard && typeof navigator.clipboard.writeText === "function") {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
  }
  try {
    const area = document.createElement("textarea");
    area.value = text;
    area.setAttribute("readonly", "");
    area.style.position = "fixed";
    area.style.top = "-1000px";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    const ok = typeof document.execCommand === "function" && document.execCommand("copy");
    area.remove();
    return Boolean(ok);
  } catch {
    return false;
  }
}
var toastTimers = /* @__PURE__ */ new WeakMap();
function showToast(text) {
  let toast = document.querySelector(".dsh-fnos-advanced-toast");
  if (!toast) {
    toast = document.createElement("div");
    toast.className = "dsh-fnos-advanced-toast";
    toast.setAttribute("role", "status");
    document.body.appendChild(toast);
  }
  toast.textContent = text;
  toast.style.display = "block";
  const previous = toastTimers.get(toast);
  if (previous) window.clearTimeout(previous);
  toastTimers.set(toast, window.setTimeout(() => {
    toast.style.display = "none";
  }, 5e3));
}
function fillAndEnter(input, path) {
  input.focus();
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, "value")?.set;
  if (setter) setter.call(input, path);
  else input.value = path;
  input.dispatchEvent(new Event("input", { bubbles: true }));
  input.dispatchEvent(new Event("change", { bubbles: true }));
  window.setTimeout(() => {
    input.focus();
    const options = {
      key: "Enter",
      code: "Enter",
      keyCode: 13,
      which: 13,
      charCode: 13,
      bubbles: true,
      cancelable: true
    };
    input.dispatchEvent(new KeyboardEvent("keydown", options));
    input.dispatchEvent(new KeyboardEvent("keypress", options));
    input.dispatchEvent(new KeyboardEvent("keyup", options));
  }, 0);
}
function dismissTriggerMenu() {
  try {
    const event = typeof PointerEvent === "function" ? new PointerEvent("pointerdown", { bubbles: true, cancelable: true }) : new MouseEvent("mousedown", { bubbles: true, cancelable: true });
    (document.body || document.documentElement).dispatchEvent(event);
  } catch {
  }
}
function closePopup(popup) {
  if (!popup) return;
  try {
    popup.close();
  } catch {
  }
}
function writePopupLoading(popup) {
  try {
    popup.document.title = t("飞牛授权");
    popup.document.body.innerHTML = `<p style="font:14px/1.6 -apple-system,BlinkMacSystemFont,'PingFang SC',sans-serif;padding:24px;color:#555">${t("正在打开飞牛授权页面…")}</p>`;
  } catch {
  }
}

// src/client/files.ts
var IMAGE_MIME_BY_EXTENSION = Object.freeze({
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".webp": "image/webp",
  ".gif": "image/gif"
});
function fileExtension(value) {
  const text = String(value ?? "");
  const base = text.slice(Math.max(text.lastIndexOf("/"), text.lastIndexOf("\\")) + 1);
  const dot = base.lastIndexOf(".");
  if (dot <= 0 || dot === base.length - 1) return "";
  return base.slice(dot).toLowerCase();
}
function imageMimeForPath(value) {
  return IMAGE_MIME_BY_EXTENSION[fileExtension(value)] || "";
}
var UNSUPPORTED_IMAGE_FORMATS = Object.freeze({
  ".heic": "HEIC",
  ".heif": "HEIF",
  ".avif": "AVIF",
  ".tif": "TIFF",
  ".tiff": "TIFF",
  ".bmp": "BMP",
  ".svg": "SVG",
  ".raw": "RAW",
  ".dng": "DNG",
  ".cr2": "CR2",
  ".nef": "NEF",
  ".arw": "ARW"
});
function unsupportedImageFormat(value) {
  return UNSUPPORTED_IMAGE_FORMATS[fileExtension(value)] || "";
}
function resolveAbsolutePath(cwd, value) {
  const text = String(value ?? "");
  if (!text) return "";
  if (text.startsWith("/")) return text;
  const root = String(cwd ?? "");
  if (root === "/") return `/${text.replace(/^\.\//, "")}`;
  const base = root.replace(/\/+$/, "");
  if (!base.startsWith("/")) return "";
  return `${base}/${text.replace(/^\.\//, "")}`;
}
function fileNameForPath(value) {
  const text = String(value ?? "").replace(/[\\/]+$/, "");
  const base = text.slice(Math.max(text.lastIndexOf("/"), text.lastIndexOf("\\")) + 1);
  return base || "file";
}

// src/client/host-paths.ts
var pluginPaths = /* @__PURE__ */ new WeakMap();
var activeStore = pluginPaths;
function setHostPath(file, path) {
  activeStore.set(file, path);
}
function ensureHostPathBridge() {
  const existing = globalThis.__DSH_HOST_PATHS__;
  if (existing && existing.__dshFnosAdvanced) {
    activeStore = existing.__dshFnosFiles || activeStore;
    return existing;
  }
  if (existing && typeof existing === "object") {
    const original = typeof existing.pathFor === "function" ? existing.pathFor : null;
    const store = existing.__dshFnosFiles || /* @__PURE__ */ new WeakMap();
    try {
      existing.pathFor = function pathFor(file) {
        const own = store.get(file);
        if (own) return own;
        try {
          return original ? original.call(existing, file) : "";
        } catch {
          return "";
        }
      };
      existing.__dshFnosFiles = store;
      existing.__dshFnosAdvanced = true;
      activeStore = store;
      return existing;
    } catch {
    }
  }
  const bridge = {
    __dshFnosAdvanced: true,
    __dshFnosFiles: pluginPaths,
    pathFor(file) {
      const own = pluginPaths.get(file);
      if (own) return own;
      try {
        return existing && typeof existing.pathFor === "function" ? existing.pathFor(file) : "";
      } catch {
        return "";
      }
    }
  };
  try {
    globalThis.__DSH_HOST_PATHS__ = bridge;
  } catch {
  }
  activeStore = pluginPaths;
  return bridge;
}
function dispatchComposerFiles(input, files) {
  const transfer = new DataTransfer();
  for (const file of files) transfer.items.add(file);
  input.files = transfer.files;
  input.dispatchEvent(new Event("change", { bubbles: true }));
}

// src/shared/settings.ts
var DEFAULT_SETTINGS = Object.freeze({
  proxyBase: "",
  lanTarget: "5666",
  authMode: "ask",
  imageRead: true
});
var AUTH_MODES = Object.freeze(["ask", "proxy", "lan"]);
function isAuthMode(value) {
  return typeof value === "string" && AUTH_MODES.includes(value);
}
function normalizeBase(value) {
  return String(value ?? "").replace(/\/+$/, "");
}

// src/client/plugin-api.ts
function pluginURL(relative) {
  const base = document.baseURI.endsWith("/") ? document.baseURI : `${document.baseURI}/`;
  return new URL(relative, base).href;
}
var configPromise = null;
function loadConfig() {
  if (!configPromise) {
    configPromise = (async () => {
      try {
        const response = await fetch(pluginURL("fnos-advanced/config"), {
          credentials: "same-origin",
          cache: "no-store",
          headers: { Accept: "application/json" }
        });
        if (response.ok) {
          const data = await response.json();
          if (data && typeof data === "object") return data;
        }
      } catch {
      }
      return { appName: "Harness", authBase: "" };
    })();
  }
  return configPromise;
}
var settingsCache = null;
var settingsCachedAt = 0;
var settingsPromise = null;
var hostState = "unknown";
function getSettingsHostState() {
  return hostState;
}
function normalizeClientSettings(input) {
  const record = typeof input === "object" && input !== null ? input : {};
  return {
    proxyBase: normalizeBase(typeof record.proxyBase === "string" ? record.proxyBase : ""),
    lanTarget: typeof record.lanTarget === "string" && record.lanTarget.trim() ? record.lanTarget.trim() : DEFAULT_SETTINGS.lanTarget,
    authMode: isAuthMode(record.authMode) ? record.authMode : DEFAULT_SETTINGS.authMode,
    imageRead: record.imageRead !== false
  };
}
async function loadSettings(force = false) {
  if (!force && settingsCache && Date.now() - settingsCachedAt < 5e3) return settingsCache;
  if (!force && settingsPromise) return settingsPromise;
  const request = (async () => {
    try {
      const response = await fetch(pluginURL("fnos-advanced/settings"), {
        credentials: "same-origin",
        cache: "no-store",
        headers: { Accept: "application/json" }
      });
      if (response.ok) {
        const data = await response.json();
        if (data && data.ok && data.settings) {
          settingsCache = normalizeClientSettings(data.settings);
          settingsCachedAt = Date.now();
          hostState = "ok";
          return settingsCache;
        }
        hostState = "error";
      } else if (response.status === 404 || response.status === 405) {
        hostState = "missing";
      } else {
        hostState = "error";
      }
    } catch {
      hostState = "error";
    }
    return settingsCache || { ...DEFAULT_SETTINGS };
  })();
  settingsPromise = request;
  try {
    return await request;
  } finally {
    if (settingsPromise === request) settingsPromise = null;
  }
}
async function saveSettings(input) {
  const proxyBase = validateProxyBase(input.proxyBase);
  const response = await fetch(pluginURL("fnos-advanced/settings"), {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify({
      proxyBase,
      lanTarget: String(input.lanTarget || "").trim() || DEFAULT_SETTINGS.lanTarget,
      authMode: input.authMode,
      imageRead: input.imageRead !== false
    })
  });
  if (response.status === 404 || response.status === 405) {
    throw new Error(t("宿主半侧未提供设置接口，通常是插件升级后没有重启 dsh。请重启 dsh 后再保存设置。"));
  }
  const data = await response.json().catch(() => ({}));
  if (!response.ok || !data.ok) {
    throw new Error(translateError(data.error) || t("保存失败（HTTP {status}）", { status: response.status }));
  }
  const saved = normalizeClientSettings(data.settings);
  settingsCache = saved;
  settingsCachedAt = Date.now();
  return saved;
}
async function fetchPluginHealth() {
  try {
    const response = await fetch(pluginURL("fnos-advanced/health"), {
      credentials: "same-origin",
      cache: "no-store"
    });
    if (!response.ok) return null;
    return await response.json();
  } catch {
    return null;
  }
}
var workspacePaths = /* @__PURE__ */ new Map();
function setWorkspacePaths(entries) {
  workspacePaths = new Map(entries);
}
async function knownWorkspacePaths() {
  if (workspacePaths.size === 0) {
    try {
      const response = await fetch(pluginURL("fnos-advanced/workspaces"), {
        credentials: "same-origin",
        cache: "no-store",
        headers: { Accept: "application/json" }
      });
      if (response.ok) {
        const data = await response.json();
        const items = data && Array.isArray(data.workspaces) ? data.workspaces : [];
        for (const workspace of items) {
          if (workspace && workspace.id && typeof workspace.path === "string") {
            workspacePaths.set(String(workspace.id), workspace.path);
          }
        }
      }
    } catch {
    }
  }
  return [...new Set([...workspacePaths.values()].filter(Boolean))];
}
async function resolveWorkspacePath(id) {
  const cached = workspacePaths.get(id);
  if (cached) return cached;
  try {
    const response = await fetch(
      `${pluginURL("fnos-advanced/workspace-path")}?id=${encodeURIComponent(id)}`,
      { credentials: "same-origin", cache: "no-store", headers: { Accept: "application/json" } }
    );
    if (response.ok) {
      const data = await response.json();
      if (data && data.ok && typeof data.path === "string" && data.path) {
        workspacePaths.set(id, data.path);
        return data.path;
      }
    }
  } catch {
  }
  return "";
}
async function checkWorkspaceHealth(ids) {
  const response = await fetch(pluginURL("fnos-advanced/workspace-health"), {
    method: "POST",
    credentials: "same-origin",
    cache: "no-store",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify({ ids: Array.isArray(ids) ? ids : [] })
  });
  if (response.status === 404 || response.status === 405) {
    throw new Error(t("没有找到工作区权限接口"));
  }
  const data = await response.json().catch(() => ({}));
  if (!response.ok || !data.ok) {
    throw new Error(translateError(data.error) || t("检查失败：{error}", { error: `HTTP ${response.status}` }));
  }
  return Array.isArray(data.results) ? data.results : [];
}
function workspaceHealthText(result) {
  if (!result || !result.exists) return t("目录不存在");
  if (result.directory === false) return t("不是目录");
  if (!result.readable) return t("目录不可读（{error}）", { error: result.error || "EACCES" });
  if (!result.writable) return t("目录可读但不可写");
  return t("目录可读写");
}
async function readImageFile(path, mime) {
  const tokenResponse = await fetch(pluginURL("fnos-advanced/file-token"), {
    method: "POST",
    credentials: "same-origin",
    cache: "no-store",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify({ path })
  });
  if (tokenResponse.status === 404 || tokenResponse.status === 405) {
    throw new Error(t("宿主半侧未提供图片读取接口"));
  }
  const info = await tokenResponse.json().catch(() => ({}));
  if (!tokenResponse.ok || !info.ok || !info.token) {
    throw new Error(translateError(info.error) || t("读取图片失败（HTTP {status}）", { status: tokenResponse.status }));
  }
  const response = await fetch(
    pluginURL(`fnos-advanced/file-bytes?token=${encodeURIComponent(info.token)}`),
    { credentials: "same-origin", cache: "no-store" }
  );
  if (!response.ok) throw new Error(t("读取图片失败（HTTP {status}）", { status: response.status }));
  const blob = await response.blob();
  return new File([blob], info.name || fileNameForPath(path), {
    type: info.mime || mime || blob.type || "application/octet-stream"
  });
}
async function isGatewayAccess(appName) {
  if (/^\/app(?:\/|$)/.test(window.location.pathname)) return true;
  return authRouteExists(appName);
}
async function authRouteExists(appName) {
  try {
    const response = await fetch(
      `${window.location.origin}/app-auth/pick-user-file?appName=${encodeURIComponent(appName || "Harness")}&directory=true`,
      {
        method: "GET",
        credentials: "include",
        cache: "no-store",
        redirect: "manual"
      }
    );
    if (response.type === "opaqueredirect") return true;
    return response.status !== 404 && response.status !== 502 && response.status !== 400;
  } catch {
    return false;
  }
}
function lanBaseFromSettings(settings) {
  const target = String(settings.lanTarget || "").trim() || DEFAULT_SETTINGS.lanTarget;
  if (/^\d+$/.test(target)) {
    const port = Number(target);
    if (!Number.isInteger(port) || port < 1 || port > 65535) return "";
    return `http://${window.location.hostname}:${port}`;
  }
  try {
    const url = new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(target) ? target : `http://${target}`);
    if (url.protocol !== "http:" && url.protocol !== "https:") return "";
    return normalizeBase(url.origin);
  } catch {
    return "";
  }
}
async function resolveAuthTargets(config, settings) {
  const appName = config.appName || "Harness";
  if (await isGatewayAccess(appName)) {
    return { mode: "gateway", base: window.location.origin, via: "gateway" };
  }
  const proxy = normalizeBase(settings.proxyBase || "");
  const lan = lanBaseFromSettings(settings);
  const choices = [];
  if (proxy) {
    choices.push({ id: "proxy", label: t("通过反代地址"), detail: proxy, base: proxy });
  }
  if (lan && !choices.some((choice) => choice.base === lan)) {
    choices.push({ id: "lan", label: t("通过内网地址"), detail: lan, base: lan });
  }
  if (settings.authMode === "proxy" && proxy) {
    return { mode: "single", base: proxy, via: "proxy" };
  }
  if (settings.authMode === "lan" && lan) {
    return { mode: "single", base: lan, via: "lan" };
  }
  if (choices.length > 1) return { mode: "choose", choices };
  const [only] = choices;
  if (only) return { mode: "single", base: only.base, via: only.id };
  return { mode: "single", base: resolveAuthBase(), via: "auto" };
}
function readStoredAuthBase() {
  try {
    return localStorage.getItem(AUTH_BASE_KEY) || "";
  } catch {
    return "";
  }
}
function resolveAuthBase() {
  const stored = readStoredAuthBase();
  if (stored) return normalizeBase(stored);
  const { origin, pathname, protocol, hostname, port } = window.location;
  if (/^\/app(?:\/|$)/.test(pathname)) return origin;
  if (protocol === "https:" && (port === "" || port === "443")) return origin;
  if (protocol === "http:" || protocol === "https:") {
    return `${protocol}//${hostname}:5666`;
  }
  return origin;
}
function validateProxyBase(value) {
  const text = String(value || "").trim();
  if (!text) return "";
  const url = new URL(text);
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error(t("反代地址只支持 http/https"));
  }
  return url.origin;
}
function getCachedSettings() {
  return settingsCache ?? { ...DEFAULT_SETTINGS };
}

// src/client/composer.ts
function findComposerFileInput(region) {
  const inputs = Array.from(region.querySelectorAll('input[type="file"]'));
  return inputs.find((input) => input.multiple && !input.disabled) || null;
}
function findActiveComposerInput() {
  for (const region of document.querySelectorAll('[data-conversation-region="composer"]')) {
    if (!isVisible(region)) continue;
    const input = findComposerFileInput(region);
    if (input) return { input, region };
  }
  return null;
}
async function addPathsToComposer(paths, input) {
  const settings = await loadSettings();
  ensureHostPathBridge();
  const files = [];
  let images = 0;
  let unreadable = 0;
  let unsupported = 0;
  let disabled = 0;
  for (const raw of paths) {
    const path = String(raw);
    const mime = imageMimeForPath(path);
    if (mime && settings.imageRead !== false) {
      try {
        const file2 = await readImageFile(path, mime);
        setHostPath(file2, path);
        files.push(file2);
        images += 1;
        continue;
      } catch (error) {
        console.warn(LOG_PREFIX, "image bytes unavailable; adding a path reference instead", error);
        unreadable += 1;
      }
    } else if (mime) {
      disabled += 1;
    } else if (unsupportedImageFormat(path)) {
      unsupported += 1;
    }
    const file = new File([], fileNameForPath(path), { type: "application/octet-stream" });
    setHostPath(file, path);
    files.push(file);
  }
  if (!files.length) throw new Error(t("没有可添加的文件"));
  dispatchComposerFiles(input, files);
  const notices = [];
  if (unreadable > 0) {
    notices.push(t("{count} 张图片读取失败（可能超过 8 MB），已改为路径引用", { count: unreadable }));
  }
  if (unsupported > 0) {
    notices.push(t("{count} 张图片格式不支持视觉（如 HEIC），已改为路径引用", { count: unsupported }));
  }
  if (disabled > 0) {
    notices.push(t("{count} 张图片未读取（设置已关闭图片读取），已改为路径引用", { count: disabled }));
  }
  if (notices.length) {
    showToast(notices.join(" · "));
  } else if (images > 0) {
    showToast(t("已添加 {count} 个文件（{images} 张图片会上传）", { count: files.length, images }));
  }
}
async function quotePathToComposer(path) {
  const active = findActiveComposerInput();
  if (!active) {
    showToast(t("没有找到对话输入框，无法引用。"));
    return;
  }
  await addPathsToComposer([path], active.input);
}

// src/client/menu.ts
function looksLikeComposerCommandMenu(labels) {
  const text = labels.map((value) => String(value ?? "").toLowerCase());
  if (text.length < 3) return false;
  return text.some((item) => /\bfile\b/.test(item)) && text.some((item) => /\bgoal\b/.test(item)) && text.some((item) => /\bplan\b/.test(item));
}

// node_modules/@trimjs/web-app/dist/index.js
function e(t3) {
  return e = "function" == typeof Symbol && "symbol" == typeof Symbol.iterator ? function(e2) {
    return typeof e2;
  } : function(e2) {
    return e2 && "function" == typeof Symbol && e2.constructor === Symbol && e2 !== Symbol.prototype ? "symbol" : typeof e2;
  }, e(t3);
}
function t2(t3) {
  var r2 = (function(t4, r3) {
    if ("object" != e(t4) || !t4) return t4;
    var n2 = t4[Symbol.toPrimitive];
    if (void 0 !== n2) {
      var i2 = n2.call(t4, r3 || "default");
      if ("object" != e(i2)) return i2;
      throw new TypeError("@@toPrimitive must return a primitive value.");
    }
    return ("string" === r3 ? String : Number)(t4);
  })(t3, "string");
  return "symbol" == e(r2) ? r2 : r2 + "";
}
function r(e2, r2, n2) {
  return (r2 = t2(r2)) in e2 ? Object.defineProperty(e2, r2, { value: n2, enumerable: true, configurable: true, writable: true }) : e2[r2] = n2, e2;
}
var n = new class {
  constructor() {
    r(this, "debug", false);
  }
  setDebug(e2) {
    this.debug = e2;
  }
  isDebug() {
    return this.debug;
  }
  log() {
    if (this.debug) {
      for (var e2 = arguments.length, t3 = new Array(e2), r2 = 0; r2 < e2; r2++) t3[r2] = arguments[r2];
      console.log("[Trim App]", ...t3);
    }
  }
  warn() {
    if (this.debug) {
      for (var e2 = arguments.length, t3 = new Array(e2), r2 = 0; r2 < e2; r2++) t3[r2] = arguments[r2];
      console.warn("[Trim App]", ...t3);
    }
  }
  error() {
    if (this.debug) {
      for (var e2 = arguments.length, t3 = new Array(e2), r2 = 0; r2 < e2; r2++) t3[r2] = arguments[r2];
      console.error("[Trim App]", ...t3);
    }
  }
  info() {
    if (this.debug) {
      for (var e2 = arguments.length, t3 = new Array(e2), r2 = 0; r2 < e2; r2++) t3[r2] = arguments[r2];
      console.info("[Trim App]", ...t3);
    }
  }
}();
var i = () => (function(e2) {
  const t3 = /FNOS\/([\d.]+)|FNAppType\/(\w+)|FNAppVer\/([\d.]+)/g, r2 = {};
  let n2;
  for (; null !== (n2 = t3.exec(e2)); ) n2[1] && (r2.fnOSVersion = n2[1]), n2[2] && (r2.fnAppType = n2[2]), n2[3] && (r2.fnAppVersion = n2[3]);
  return r2;
})(navigator.userAgent).fnAppVersion;
function o(e2, t3, r2, n2, i2, o2, s2) {
  try {
    var l2 = e2[o2](s2), a2 = l2.value;
  } catch (e3) {
    return void r2(e3);
  }
  l2.done ? t3(a2) : Promise.resolve(a2).then(n2, i2);
}
function s(e2) {
  return function() {
    var t3 = this, r2 = arguments;
    return new Promise(function(n2, i2) {
      var s2 = e2.apply(t3, r2);
      function l2(e3) {
        o(s2, n2, i2, l2, a2, "next", e3);
      }
      function a2(e3) {
        o(s2, n2, i2, l2, a2, "throw", e3);
      }
      l2(void 0);
    });
  };
}
function l(e2, t3) {
  var r2 = Object.keys(e2);
  if (Object.getOwnPropertySymbols) {
    var n2 = Object.getOwnPropertySymbols(e2);
    t3 && (n2 = n2.filter(function(t4) {
      return Object.getOwnPropertyDescriptor(e2, t4).enumerable;
    })), r2.push.apply(r2, n2);
  }
  return r2;
}
function a(e2) {
  for (var t3 = 1; t3 < arguments.length; t3++) {
    var n2 = null != arguments[t3] ? arguments[t3] : {};
    t3 % 2 ? l(Object(n2), true).forEach(function(t4) {
      r(e2, t4, n2[t4]);
    }) : Object.getOwnPropertyDescriptors ? Object.defineProperties(e2, Object.getOwnPropertyDescriptors(n2)) : l(Object(n2)).forEach(function(t4) {
      Object.defineProperty(e2, t4, Object.getOwnPropertyDescriptor(n2, t4));
    });
  }
  return e2;
}
var u = false;
window.addEventListener("flutterInAppWebViewPlatformReady", () => {
  u = true;
});
var c = /(iPad|iPhone|iPod|Macintosh|Mac OS X)/i.test(navigator.userAgent);
var p = /* @__PURE__ */ (function() {
  var e2 = s(function* (e3, t3) {
    for (var r2 = arguments.length, i2 = new Array(r2 > 2 ? r2 - 2 : 0), o2 = 2; o2 < r2; o2++) i2[o2 - 2] = arguments[o2];
    n.log(`callHandler: ${t3}, params: ${i2.map((e4) => String(e4)).join(", ")}`);
    const s2 = yield e3.callHandler(t3, ...i2);
    if (s2) try {
      const e4 = JSON.parse(s2);
      n.log(`messageData: ${JSON.stringify(e4)}`);
      const t4 = JSON.parse(e4.result);
      return n.log(`appMessage: ${JSON.stringify(t4)}`), t4;
    } catch (l2) {
      return n.error(`error: ${l2}`), null;
    }
    return null;
  });
  return function(t3, r2) {
    return e2.apply(this, arguments);
  };
})();
var d = /* @__PURE__ */ new Map();
var f = class {
  constructor(e2) {
    r(this, "subscribeFn", void 0), this.subscribeFn = e2;
  }
  subscribe(e2, t3, r2) {
    var n2;
    const i2 = "function" == typeof e2 ? { next: e2, error: t3, complete: r2 } : null != e2 ? e2 : {};
    let o2 = false;
    const s2 = null !== (n2 = this.subscribeFn({ next: (e3) => {
      var t4;
      o2 || (null === (t4 = i2.next) || void 0 === t4 || t4.call(i2, e3));
    }, error: (e3) => {
      var t4;
      o2 || (o2 = true, null === (t4 = i2.error) || void 0 === t4 || t4.call(i2, e3));
    }, complete: () => {
      var e3;
      o2 || (o2 = true, null === (e3 = i2.complete) || void 0 === e3 || e3.call(i2));
    } })) && void 0 !== n2 ? n2 : () => {
    };
    return { unsubscribe: () => {
      o2 || (o2 = true, s2());
    } };
  }
};
var h = /* @__PURE__ */ new Map();
function v() {
  window._flutter_onAppObservableQueryResult = (e2) => {
    n.log("_flutter_onAppObservableQueryResult", e2);
    const t3 = (function(e3) {
      var t4, r3;
      const n2 = "string" == typeof e3 ? JSON.parse(e3) : e3, i2 = null !== (t4 = n2.reqid) && void 0 !== t4 ? t4 : n2.reqId;
      return a(a({}, n2), {}, { reqId: null !== (r3 = n2.reqId) && void 0 !== r3 ? r3 : i2, reqid: i2 });
    })(e2), r2 = h.get(t3.reqId);
    if (r2) if (t3.result && "doing" !== t3.result) {
      if ("succ" === t3.result) return r2.next(t3), r2.complete(), void h.delete(t3.reqId);
      r2.error(t3), h.delete(t3.reqId);
    } else r2.next(t3);
    else n.log("_flutter_onAppObservableQueryResult observer not found", t3);
  };
}
function y() {
  return (y = s(function* (e2, t3) {
    return new Promise((r2, i2) => {
      null == e2 || e2.callHandler("openFolder", JSON.stringify(t3)).then((e3) => {
        if (n.log("pickFilePromise callback registered", { reqId: e3, params: t3 }), e3) {
          let o2 = function(e4) {
            r2(e4.result);
          };
          d.set(e3, o2);
        } else i2(/* @__PURE__ */ new Error("openFolder failed"));
      });
    });
  })).apply(this, arguments);
}
i() && (window.fnAppMessage = (e2) => {
  n.log("app-message", e2);
  const t3 = JSON.parse(e2), r2 = d.get(t3.reqId);
  r2 && (r2(t3), d.delete(t3.reqId));
}), i() && v();
var g = /* @__PURE__ */ (function(e2) {
  return e2.Call = "call", e2.Reply = "reply", e2.Syn = "syn", e2.SynAck = "synAck", e2.Ack = "ack", e2;
})({});
var b = /* @__PURE__ */ (function(e2) {
  return e2.Fulfilled = "fulfilled", e2.Rejected = "rejected", e2;
})({});
var m = /* @__PURE__ */ (function(e2) {
  return e2.ConnectionDestroyed = "ConnectionDestroyed", e2.ConnectionTimeout = "ConnectionTimeout", e2.NoIframeSrc = "NoIframeSrc", e2;
})({});
var w = /* @__PURE__ */ (function(e2) {
  return e2.DataCloneError = "DataCloneError", e2;
})({});
var A = /* @__PURE__ */ (function(e2) {
  return e2.Message = "message", e2;
})({});
var S = ({ name: e2, message: t3, stack: r2 }) => ({ name: e2, message: t3, stack: r2 });
var O = (e2, t3, r2) => {
  let { localName: n2, local: i2, remote: o2, originForSending: s2, originForReceiving: l2 } = e2, a2 = false, u2 = (e3) => {
    if (e3.source !== o2 || e3.data.penpal !== g.Call) return;
    if ("*" !== l2 && e3.origin !== l2) return void r2(`${n2} received message from origin ${e3.origin} which did not match expected origin ${l2}`);
    let { methodName: i3, args: u3, id: c2 } = e3.data;
    r2(`${n2}: Received ${i3}() call`);
    let p2 = (e4) => (t4) => {
      if (r2(`${n2}: Sending ${i3}() reply`), a2) return void r2(`${n2}: Unable to send ${i3}() reply due to destroyed connection`);
      let l3 = { penpal: g.Reply, id: c2, resolution: e4, returnValue: t4 };
      e4 === b.Rejected && t4 instanceof Error && (l3.returnValue = S(t4), l3.returnValueIsError = true);
      try {
        o2.postMessage(l3, s2);
      } catch (e5) {
        let r3 = e5 instanceof Error ? e5 : Error(String(e5));
        if (r3.name === w.DataCloneError) {
          let e6 = { penpal: g.Reply, id: c2, resolution: b.Rejected, returnValue: S(r3), returnValueIsError: true };
          o2.postMessage(e6, s2);
        }
        throw e5;
      }
    };
    new Promise((e4) => e4(t3[i3].apply(t3, u3))).then(p2(b.Fulfilled), p2(b.Rejected));
  };
  return i2.addEventListener(A.Message, u2), () => {
    a2 = true, i2.removeEventListener(A.Message, u2);
  };
};
var P = 0;
var E = () => ++P;
var M = (e2) => e2 ? e2.split(".") : [];
var F = (e2, t3, r2) => {
  let n2 = M(t3);
  return n2.reduce((e3, t4, i2) => (void 0 === e3[t4] && (e3[t4] = {}), i2 === n2.length - 1 && (e3[t4] = r2), e3[t4]), e2), e2;
};
var $ = (e2, t3) => {
  let r2 = {};
  return Object.keys(e2).forEach((n2) => {
    let i2 = e2[n2], o2 = ((e3, t4) => {
      let r3 = M(t4 || "");
      return r3.push(e3), ((e4) => e4.join("."))(r3);
    })(n2, t3);
    "object" == typeof i2 && Object.assign(r2, $(i2, o2)), "function" == typeof i2 && (r2[o2] = i2);
  }), r2;
};
var _ = (e2, t3, r2, n2, i2) => {
  let { localName: o2, local: s2, remote: l2, originForSending: a2, originForReceiving: u2 } = t3, c2 = false, p2 = /* @__PURE__ */ new Set();
  i2(`${o2}: Connecting call sender`);
  let d2 = (e3) => (...r3) => {
    let d3;
    i2(`${o2}: Sending ${e3}() call`);
    try {
      l2.closed && (d3 = true);
    } catch (g2) {
      d3 = true;
    }
    if (d3 && n2(), c2) {
      let t4 = /* @__PURE__ */ Error(`Unable to send ${e3}() call due to destroyed connection`);
      throw t4.code = m.ConnectionDestroyed, t4;
    }
    return new Promise((n3, c3) => {
      var d4;
      let f3, h2 = E(), v2 = () => {
        s2.removeEventListener(A.Message, m2), void 0 !== f3 && clearTimeout(f3), p2.delete(y2);
      }, y2 = () => {
        v2(), c3(/* @__PURE__ */ Error("Connection closed"));
      }, m2 = (t4) => {
        var r4;
        if (t4.source !== l2 || (null == (r4 = t4.data) ? void 0 : r4.penpal) !== g.Reply || t4.data.id !== h2) return;
        if ("*" !== u2 && t4.origin !== u2) return void i2(`${o2} received message from origin ${t4.origin} which did not match expected origin ${u2}`);
        let s3 = t4.data;
        i2(`${o2}: Received ${e3}() reply`), v2();
        let a3 = s3.returnValue;
        s3.returnValueIsError && (a3 = ((e4) => {
          let t5 = /* @__PURE__ */ Error();
          return Object.assign(t5, e4), t5;
        })(a3)), (s3.resolution === b.Fulfilled ? n3 : c3)(a3);
      };
      s2.addEventListener(A.Message, m2), p2.add(y2);
      let w2 = null == (d4 = t3.callTimeouts) ? void 0 : d4[e3];
      void 0 !== w2 && (f3 = setTimeout(y2, w2));
      let S2 = { penpal: g.Call, id: h2, methodName: e3, args: r3 };
      try {
        l2.postMessage(S2, a2);
      } catch (g2) {
        v2(), c3(g2);
      }
    });
  }, f2 = r2.reduce((e3, t4) => (e3[t4] = d2(t4), e3), {});
  return Object.assign(e2, ((e3) => {
    let t4 = {};
    for (let r3 in e3) F(t4, r3, e3[r3]);
    return t4;
  })(f2)), () => {
    c2 = true;
    for (let e3 of p2) e3();
  };
};
var k = (e2, t3) => {
  let r2;
  return void 0 !== e2 && (r2 = window.setTimeout(() => {
    let r3 = /* @__PURE__ */ Error(`Connection timed out after ${e2}ms`);
    r3.code = m.ConnectionTimeout, t3(r3);
  }, e2)), () => {
    clearTimeout(r2);
  };
};
function T(e2) {
  return T = "function" == typeof Symbol && "symbol" == typeof Symbol.iterator ? function(e3) {
    return typeof e3;
  } : function(e3) {
    return e3 && "function" == typeof Symbol && e3.constructor === Symbol && e3 !== Symbol.prototype ? "symbol" : typeof e3;
  }, T(e2);
}
function j(e2) {
  var t3 = (function(e3, t4) {
    if ("object" != T(e3) || !e3) return e3;
    var r2 = e3[Symbol.toPrimitive];
    if (void 0 !== r2) {
      var n2 = r2.call(e3, t4 || "default");
      if ("object" != T(n2)) return n2;
      throw TypeError("@@toPrimitive must return a primitive value.");
    }
    return ("string" === t4 ? String : Number)(e3);
  })(e2, "string");
  return "symbol" == T(t3) ? t3 : t3 + "";
}
function N(e2, t3, r2) {
  return (t3 = j(t3)) in e2 ? Object.defineProperty(e2, t3, { value: r2, enumerable: true, configurable: true, writable: true }) : e2[t3] = r2, e2;
}
function W(e2, t3) {
  var r2 = Object.keys(e2);
  if (Object.getOwnPropertySymbols) {
    var n2 = Object.getOwnPropertySymbols(e2);
    t3 && (n2 = n2.filter(function(t4) {
      return Object.getOwnPropertyDescriptor(e2, t4).enumerable;
    })), r2.push.apply(r2, n2);
  }
  return r2;
}
function C(e2) {
  for (var t3 = 1; t3 < arguments.length; t3++) {
    var r2 = null == arguments[t3] ? {} : arguments[t3];
    t3 % 2 ? W(Object(r2), true).forEach(function(t4) {
      N(e2, t4, r2[t4]);
    }) : Object.getOwnPropertyDescriptors ? Object.defineProperties(e2, Object.getOwnPropertyDescriptors(r2)) : W(Object(r2)).forEach(function(t4) {
      Object.defineProperty(e2, t4, Object.getOwnPropertyDescriptor(r2, t4));
    });
  }
  return e2;
}
var q = (e2, t3, r2, n2, i2, o2) => {
  let { destroy: s2, onDestroy: l2 } = n2;
  return (n3) => {
    if (!(e2 instanceof RegExp ? e2.test(n3.origin) : "*" === e2 || e2 === n3.origin)) return void i2(`Child: Handshake - Received SYN-ACK from origin ${n3.origin} which did not match expected origin ${e2}`);
    i2("Child: Handshake - Received SYN-ACK, responding with ACK");
    let a2 = "null" === n3.origin ? "*" : n3.origin, u2 = { penpal: g.Ack, methodNames: Object.keys(t3), config: r2 };
    window.parent.postMessage(u2, a2);
    let c2 = { callTimeouts: o2, localName: "Child", local: window, remote: window.parent, originForSending: a2, originForReceiving: n3.origin }, p2 = O(c2, t3, i2);
    l2(p2);
    let d2 = {}, f2 = _(d2, c2, n3.data.methodNames, s2, i2);
    return l2(f2), d2;
  };
};
function x(e2, t3) {
  var r2;
  null == (r2 = console) || r2.warn(`[fnApp warn]: ${e2}`, t3);
}
function I(e2, t3) {
  var r2;
  null == (r2 = console) || r2.error(`[fnApp error]: ${e2}`, t3);
}
var U = "_fn_all_event";
var V = "事件订阅数量为空";
function R(e2) {
  return R = "function" == typeof Symbol && "symbol" == typeof Symbol.iterator ? function(e3) {
    return typeof e3;
  } : function(e3) {
    return e3 && "function" == typeof Symbol && e3.constructor === Symbol && e3 !== Symbol.prototype ? "symbol" : typeof e3;
  }, R(e2);
}
function D(e2) {
  var t3 = (function(e3, t4) {
    if ("object" != R(e3) || !e3) return e3;
    var r2 = e3[Symbol.toPrimitive];
    if (void 0 !== r2) {
      var n2 = r2.call(e3, t4 || "default");
      if ("object" != R(n2)) return n2;
      throw TypeError("@@toPrimitive must return a primitive value.");
    }
    return ("string" === t4 ? String : Number)(e3);
  })(e2, "string");
  return "symbol" == R(t3) ? t3 : t3 + "";
}
function z(e2, t3, r2) {
  return (t3 = D(t3)) in e2 ? Object.defineProperty(e2, t3, { value: r2, enumerable: true, configurable: true, writable: true }) : e2[t3] = r2, e2;
}
var L = window.__POWERED_BY_FNAPP__ ? window.__FNAPP.inject.appEventObjMap : /* @__PURE__ */ new Map();
var J = class {
  constructor(e2, t3) {
    z(this, "id", void 0), z(this, "appName", void 0), z(this, "eventObj", void 0), z(this, "getEventCallBackLength", (e3) => {
      var t4;
      return (null == (t4 = this.eventObj[e3]) ? void 0 : t4.length) || 0;
    }), this.id = e2, this.appName = t3, this.$clear(), L.get(this.id) || L.set(this.id, {}), this.eventObj = L.get(this.id);
  }
  $on(e2, t3) {
    let r2 = this.eventObj[e2];
    return r2 ? (r2.includes(t3) || r2.push(t3), this) : (this.eventObj[e2] = [t3], this);
  }
  $onAll(e2) {
    return this.$on(U, e2);
  }
  $once(e2, t3) {
    t3.$__once = true, this.$on(e2, t3);
  }
  $off(e2, t3) {
    let r2 = this.eventObj[e2];
    if (!e2 || !r2 || !r2.length) return x(`${e2} ${V}`), this;
    let n2, i2 = r2.length;
    for (; i2--; ) if (n2 = r2[i2], n2 === t3) {
      r2.splice(i2, 1);
      break;
    }
    return this;
  }
  $offAll(e2) {
    return this.$off(U, e2);
  }
  $emit(e2, ...t3) {
    return e2 = `${this.appName}/${e2}`, this.$pureEmit(e2, ...t3);
  }
  $pureEmit(e2, ...t3) {
    let r2 = 0;
    return L.forEach((n2) => {
      if (n2[e2]) for (let i2 = n2[e2].length - 1; i2 >= 0; i2--) {
        r2++;
        let o2 = n2[e2][i2];
        o2.$__once && n2[e2].splice(i2, 1);
        try {
          o2(...t3);
        } catch (e3) {
          I(e3);
        }
      }
      if (n2._fn_all_event) for (let i2 = n2[U].length - 1; i2 >= 0; i2--) {
        r2++;
        let o2 = n2[U][i2];
        o2.$__once && n2[U].splice(i2, 1);
        try {
          o2(...t3);
        } catch (e3) {
          I(e3);
        }
      }
    }), (!e2 || 0 === r2) && x(`${e2} ${V}`), this;
  }
  $clear() {
    var e2;
    let t3 = null == (e2 = L.get(this.id)) ? {} : e2;
    return Object.keys(t3).forEach((e3) => delete t3[e3]), this;
  }
};
var H = function(e2, t3) {
  return H = Object.setPrototypeOf || { __proto__: [] } instanceof Array && function(e3, t4) {
    e3.__proto__ = t4;
  } || function(e3, t4) {
    for (var r2 in t4) Object.prototype.hasOwnProperty.call(t4, r2) && (e3[r2] = t4[r2]);
  }, H(e2, t3);
};
function Q(e2, t3) {
  if ("function" != typeof t3 && null !== t3) throw TypeError("Class extends value " + String(t3) + " is not a constructor or null");
  function r2() {
    this.constructor = e2;
  }
  H(e2, t3), e2.prototype = null === t3 ? Object.create(t3) : (r2.prototype = t3.prototype, new r2());
}
function B(e2) {
  var t3 = "function" == typeof Symbol && Symbol.iterator, r2 = t3 && e2[t3], n2 = 0;
  if (r2) return r2.call(e2);
  if (e2 && "number" == typeof e2.length) return { next: function() {
    return e2 && n2 >= e2.length && (e2 = void 0), { value: e2 && e2[n2++], done: !e2 };
  } };
  throw TypeError(t3 ? "Object is not iterable." : "Symbol.iterator is not defined.");
}
function Y(e2, t3) {
  var r2 = "function" == typeof Symbol && e2[Symbol.iterator];
  if (!r2) return e2;
  var n2, i2, o2 = r2.call(e2), s2 = [];
  try {
    for (; (void 0 === t3 || t3-- > 0) && !(n2 = o2.next()).done; ) s2.push(n2.value);
  } catch (e3) {
    i2 = { error: e3 };
  } finally {
    try {
      n2 && !n2.done && (r2 = o2.return) && r2.call(o2);
    } finally {
      if (i2) throw i2.error;
    }
  }
  return s2;
}
function G(e2, t3, r2) {
  if (r2 || 2 === arguments.length) for (var n2, i2 = 0, o2 = t3.length; i2 < o2; i2++) (n2 || !(i2 in t3)) && (n2 || (n2 = Array.prototype.slice.call(t3, 0, i2)), n2[i2] = t3[i2]);
  return e2.concat(n2 || Array.prototype.slice.call(t3));
}
function K(e2) {
  return "function" == typeof e2;
}
var X = (function(e2) {
  var t3 = e2(function(e3) {
    Error.call(e3), e3.stack = Error().stack;
  });
  return t3.prototype = Object.create(Error.prototype), t3.prototype.constructor = t3, t3;
})(function(e2) {
  return function(t3) {
    e2(this), this.message = t3 ? t3.length + " errors occurred during unsubscription:\n" + t3.map(function(e3, t4) {
      return t4 + 1 + ") " + e3.toString();
    }).join("\n  ") : "", this.name = "UnsubscriptionError", this.errors = t3;
  };
});
function Z(e2, t3) {
  if (e2) {
    var r2 = e2.indexOf(t3);
    0 <= r2 && e2.splice(r2, 1);
  }
}
var ee = (function() {
  function e2(e3) {
    this.initialTeardown = e3, this.closed = false, this._parentage = null, this._finalizers = null;
  }
  return e2.prototype.unsubscribe = function() {
    var e3, t3, r2, n2, i2;
    if (!this.closed) {
      this.closed = true;
      var o2 = this._parentage;
      if (o2) if (this._parentage = null, Array.isArray(o2)) try {
        for (var s2 = B(o2), l2 = s2.next(); !l2.done; l2 = s2.next()) l2.value.remove(this);
      } catch (t4) {
        e3 = { error: t4 };
      } finally {
        try {
          l2 && !l2.done && (t3 = s2.return) && t3.call(s2);
        } finally {
          if (e3) throw e3.error;
        }
      }
      else o2.remove(this);
      var a2 = this.initialTeardown;
      if (K(a2)) try {
        a2();
      } catch (e4) {
        i2 = e4 instanceof X ? e4.errors : [e4];
      }
      var u2 = this._finalizers;
      if (u2) {
        this._finalizers = null;
        try {
          for (var c2 = B(u2), p2 = c2.next(); !p2.done; p2 = c2.next()) {
            var d2 = p2.value;
            try {
              re(d2);
            } catch (e4) {
              i2 = null == i2 ? [] : i2, e4 instanceof X ? i2 = G(G([], Y(i2)), Y(e4.errors)) : i2.push(e4);
            }
          }
        } catch (e4) {
          r2 = { error: e4 };
        } finally {
          try {
            p2 && !p2.done && (n2 = c2.return) && n2.call(c2);
          } finally {
            if (r2) throw r2.error;
          }
        }
      }
      if (i2) throw new X(i2);
    }
  }, e2.prototype.add = function(t3) {
    var r2;
    if (t3 && t3 !== this) if (this.closed) re(t3);
    else {
      if (t3 instanceof e2) {
        if (t3.closed || t3._hasParent(this)) return;
        t3._addParent(this);
      }
      (this._finalizers = null == (r2 = this._finalizers) ? [] : r2).push(t3);
    }
  }, e2.prototype._hasParent = function(e3) {
    var t3 = this._parentage;
    return t3 === e3 || Array.isArray(t3) && t3.includes(e3);
  }, e2.prototype._addParent = function(e3) {
    var t3 = this._parentage;
    this._parentage = Array.isArray(t3) ? (t3.push(e3), t3) : t3 ? [t3, e3] : e3;
  }, e2.prototype._removeParent = function(e3) {
    var t3 = this._parentage;
    t3 === e3 ? this._parentage = null : Array.isArray(t3) && Z(t3, e3);
  }, e2.prototype.remove = function(t3) {
    var r2 = this._finalizers;
    r2 && Z(r2, t3), t3 instanceof e2 && t3._removeParent(this);
  }, e2.EMPTY = (function() {
    var t3 = new e2();
    return t3.closed = true, t3;
  })(), e2;
})();
function te(e2) {
  return e2 instanceof ee || e2 && "closed" in e2 && K(e2.remove) && K(e2.add) && K(e2.unsubscribe);
}
function re(e2) {
  K(e2) ? e2() : e2.unsubscribe();
}
ee.EMPTY;
var ne = null;
var ie = null;
var oe = void 0;
var se = false;
var le = false;
var ae = { setTimeout: function(e2, t3) {
  var r2 = [...arguments].slice(2), n2 = ae.delegate;
  return null != n2 && n2.setTimeout ? n2.setTimeout.apply(n2, G([e2, t3], Y(r2))) : setTimeout.apply(void 0, G([e2, t3], Y(r2)));
}, clearTimeout: function(e2) {
  var t3 = ae.delegate;
  return ((null == t3 ? void 0 : t3.clearTimeout) || clearTimeout)(e2);
}, delegate: void 0 };
function ue() {
}
var ce = pe("C", void 0, void 0);
function pe(e2, t3, r2) {
  return { kind: e2, value: t3, error: r2 };
}
var de = null;
var fe = (function(e2) {
  function t3(t4) {
    var r2 = e2.call(this) || this;
    return r2.isStopped = false, t4 ? (r2.destination = t4, te(t4) && t4.add(r2)) : r2.destination = we, r2;
  }
  return Q(t3, e2), t3.create = function(e3, t4, r2) {
    return new ge(e3, t4, r2);
  }, t3.prototype.next = function(e3) {
    this.isStopped ? me((function(e4) {
      return pe("N", e4, void 0);
    })(e3), this) : this._next(e3);
  }, t3.prototype.error = function(e3) {
    this.isStopped ? me((function(e4) {
      return pe("E", void 0, e4);
    })(e3), this) : (this.isStopped = true, this._error(e3));
  }, t3.prototype.complete = function() {
    this.isStopped ? me(ce, this) : (this.isStopped = true, this._complete());
  }, t3.prototype.unsubscribe = function() {
    this.closed || (this.isStopped = true, e2.prototype.unsubscribe.call(this), this.destination = null);
  }, t3.prototype._next = function(e3) {
    this.destination.next(e3);
  }, t3.prototype._error = function(e3) {
    try {
      this.destination.error(e3);
    } finally {
      this.unsubscribe();
    }
  }, t3.prototype._complete = function() {
    try {
      this.destination.complete();
    } finally {
      this.unsubscribe();
    }
  }, t3;
})(ee);
var he = Function.prototype.bind;
function ve(e2, t3) {
  return he.call(e2, t3);
}
var ye = (function() {
  function e2(e3) {
    this.partialObserver = e3;
  }
  return e2.prototype.next = function(e3) {
    var t3 = this.partialObserver;
    if (t3.next) try {
      t3.next(e3);
    } catch (e4) {
      be(e4);
    }
  }, e2.prototype.error = function(e3) {
    var t3 = this.partialObserver;
    if (t3.error) try {
      t3.error(e3);
    } catch (e4) {
      be(e4);
    }
    else be(e3);
  }, e2.prototype.complete = function() {
    var e3 = this.partialObserver;
    if (e3.complete) try {
      e3.complete();
    } catch (e4) {
      be(e4);
    }
  }, e2;
})();
var ge = (function(e2) {
  function t3(t4, r2, n2) {
    var i2, o2, s2 = e2.call(this) || this;
    K(t4) || !t4 ? i2 = { next: null == t4 ? void 0 : t4, error: null == r2 ? void 0 : r2, complete: null == n2 ? void 0 : n2 } : s2 && le ? ((o2 = Object.create(t4)).unsubscribe = function() {
      return s2.unsubscribe();
    }, i2 = { next: t4.next && ve(t4.next, o2), error: t4.error && ve(t4.error, o2), complete: t4.complete && ve(t4.complete, o2) }) : i2 = t4;
    return s2.destination = new ye(i2), s2;
  }
  return Q(t3, e2), t3;
})(fe);
function be(e2) {
  se ? (function(e3) {
    se && de && (de.errorThrown = true, de.error = e3);
  })(e2) : (function(e3) {
    ae.setTimeout(function() {
      if (!ne) throw e3;
      ne(e3);
    });
  })(e2);
}
function me(e2, t3) {
  var r2 = ie;
  r2 && ae.setTimeout(function() {
    return r2(e2, t3);
  });
}
var we = { closed: true, next: ue, error: function(e2) {
  throw e2;
}, complete: ue };
var Ae = "function" == typeof Symbol && Symbol.observable || "@@observable";
function Se(e2) {
  return e2;
}
var Oe = (function() {
  function e2(e3) {
    e3 && (this._subscribe = e3);
  }
  return e2.prototype.lift = function(t3) {
    var r2 = new e2();
    return r2.source = this, r2.operator = t3, r2;
  }, e2.prototype.subscribe = function(e3, t3, r2) {
    var n2 = this, i2 = (function(e4) {
      return e4 && e4 instanceof fe || (function(e5) {
        return e5 && K(e5.next) && K(e5.error) && K(e5.complete);
      })(e4) && te(e4);
    })(e3) ? e3 : new ge(e3, t3, r2);
    return (function(e4) {
      if (se) {
        var t4 = !de;
        if (t4 && (de = { errorThrown: false, error: null }), e4(), t4) {
          var r3 = de, n3 = r3.errorThrown, i3 = r3.error;
          if (de = null, n3) throw i3;
        }
      } else e4();
    })(function() {
      var e4 = n2, t4 = e4.operator, r3 = e4.source;
      i2.add(t4 ? t4.call(i2, r3) : r3 ? n2._subscribe(i2) : n2._trySubscribe(i2));
    }), i2;
  }, e2.prototype._trySubscribe = function(e3) {
    try {
      return this._subscribe(e3);
    } catch (b2) {
      e3.error(b2);
    }
  }, e2.prototype.forEach = function(e3, t3) {
    var r2 = this;
    return new (t3 = Pe(t3))(function(t4, n2) {
      var i2 = new ge({ next: function(t5) {
        try {
          e3(t5);
        } catch (e4) {
          n2(e4), i2.unsubscribe();
        }
      }, error: n2, complete: t4 });
      r2.subscribe(i2);
    });
  }, e2.prototype._subscribe = function(e3) {
    var t3;
    return null == (t3 = this.source) ? void 0 : t3.subscribe(e3);
  }, e2.prototype[Ae] = function() {
    return this;
  }, e2.prototype.pipe = function() {
    return (function(e3) {
      return 0 === e3.length ? Se : 1 === e3.length ? e3[0] : function(t3) {
        return e3.reduce(function(e4, t4) {
          return t4(e4);
        }, t3);
      };
    })([...arguments])(this);
  }, e2.prototype.toPromise = function(e3) {
    var t3 = this;
    return new (e3 = Pe(e3))(function(e4, r2) {
      var n2;
      t3.subscribe(function(e5) {
        return n2 = e5;
      }, function(e5) {
        return r2(e5);
      }, function() {
        return e4(n2);
      });
    });
  }, e2.create = function(t3) {
    return new e2(t3);
  }, e2;
})();
function Pe(e2) {
  var t3;
  return null == (t3 = null == e2 ? oe : e2) ? Promise : t3;
}
function Ee(e2, t3, r2, n2, i2, o2, s2) {
  try {
    var l2 = e2[o2](s2), a2 = l2.value;
  } catch (e3) {
    return void r2(e3);
  }
  l2.done ? t3(a2) : Promise.resolve(a2).then(n2, i2);
}
function Me(e2) {
  return function() {
    var t3 = this, r2 = arguments;
    return new Promise(function(n2, i2) {
      var o2 = e2.apply(t3, r2);
      function s2(e3) {
        Ee(o2, n2, i2, s2, l2, "next", e3);
      }
      function l2(e3) {
        Ee(o2, n2, i2, s2, l2, "throw", e3);
      }
      s2(void 0);
    });
  };
}
var Fe = (e2 = {}) => {
  let { parentOrigin: t3 = "*", timeout: r2, debug: n2 = false, config: i2 = {} } = e2, o2 = /* @__PURE__ */ ((e3) => (...t4) => {
    e3 && console.log("[Penpal]", ...t4);
  })(n2), s2 = /* @__PURE__ */ ((e3, t4) => {
    let r3 = [], n3 = false;
    return { destroy(i3) {
      n3 || (n3 = true, t4(`${e3}: Destroying connection`), r3.forEach((e4) => {
        e4(i3);
      }));
    }, onDestroy(e4) {
      n3 ? e4() : r3.push(e4);
    } };
  })("App", o2), { destroy: l2, onDestroy: a2 } = s2;
  return { promise: new Promise((n3, u2) => {
    let c2 = k(r2, l2), p2 = (r3) => {
      if ((() => {
        try {
          clearTimeout(void 0);
        } catch (g2) {
          return false;
        }
        return true;
      })() && r3.source === parent && r3.data && r3.data.penpal === g.SynAck) {
        let l3 = r3.data, a3 = new J(l3.id, l3.appName), u3 = new J(l3.id + "/api", l3.appName), d2 = l3.id, f2 = {}, h2 = (e3) => "api/" + e3, v2 = { $emit: (e3, ...t4) => {
          a3.getEventCallBackLength(e3) > 0 ? a3.$pureEmit(e3, ...t4) : null == f2 || f2.$off(e3, () => {
          }, true);
        }, onQueryResult: (e3) => {
          u3.$pureEmit(h2(e3.reqid), e3);
        }, onOsNotify: (e3) => {
        } }, y2 = (e3) => function(t4) {
          let r4 = h2(e3);
          u3.$on(r4, function e4(n4) {
            t4.next(n4), n4.reqid && "succ" === n4.result && (t4.complete(), u3.$off(r4, e4));
          });
        }, g2 = $(v2), b2 = q(t3, g2, C(C({}, i2), {}, { postmateVersion: "0.1.5" }), s2, o2, e2.callTimeouts)(r3), m2 = {}, w2 = b2.bus;
        if (w2) {
          let e3 = (e4, t4, r4) => {
            r4 ? (t4.$__once = true, a3.$once(e4, t4)) : a3.$on(e4, t4), m2[e4] ? m2[e4].push(t4) : (m2[e4] = [t4], w2.$on(e4, d2));
          };
          Object.assign(f2, w2, { $on: e3, $off: (e4, t4, r4) => {
            var n4;
            if (m2[e4]) {
              let r5 = m2[e4].indexOf(t4);
              r5 > -1 && (m2[e4].splice(r5, 1), a3.$off(e4, t4));
            }
            r4 && delete m2[e4], null != (n4 = m2[e4]) && n4.length || (w2.$off(e4, d2), delete m2[e4]);
          }, $once: (t4, r4) => {
            e3(t4, r4, true);
          } });
        }
        let S2 = C(C({}, b2), {}, { bus: f2, query: (function() {
          var e3 = Me(function* (e4, t4) {
            let r4 = (null == e4 ? void 0 : e4.reqid) || (yield b2.genReqId());
            return new Promise((n4, i3) => {
              b2.query(C(C({}, e4), {}, { reqid: r4 }), t4).then((e5) => {
                "succ" === (null == e5 ? void 0 : e5.result) ? n4(e5) : i3(e5);
              });
            });
          });
          return function(t4, r4) {
            return e3.apply(this, arguments);
          };
        })(), createObservableQuery: (function() {
          var e3 = Me(function* (e4, t4 = {}) {
            let r4 = (null == e4 ? void 0 : e4.reqid) || (yield b2.genReqId()), n4 = new Oe(y2(r4));
            return b2.query(C(C({}, e4), {}, { reqid: r4 }), C(C({}, t4), {}, { observable: true })), n4;
          });
          return function(t4) {
            return e3.apply(this, arguments);
          };
        })() });
        b2 && (window.removeEventListener(A.Message, p2), c2(), n3({ methods: S2, config: r3.data.config }));
      }
    };
    window.addEventListener(A.Message, p2), (() => {
      o2("Child: Handshake - Sending SYN");
      let e3 = { penpal: g.Syn }, r3 = t3 instanceof RegExp ? "*" : t3;
      window.parent.postMessage(e3, r3);
    })(), a2((e3) => {
      window.removeEventListener(A.Message, p2), e3 && u2(e3);
    });
  }), destroy() {
    l2();
  } };
};
var $e = "trimjs-extension-host";
function _e() {
  return "undefined" != typeof crypto && "function" == typeof crypto.randomUUID ? crypto.randomUUID() : `web-ext-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}
function ke(e2) {
  if (!e2 || "object" != typeof e2) return false;
  const t3 = e2;
  return t3.source === $e && 1 === t3.version && "string" == typeof t3.requestId && ("request" === t3.kind || "response" === t3.kind || "event" === t3.kind);
}
function Te(e2) {
  const t3 = /* @__PURE__ */ new Error(`NotSupportedInExtensionHost: ${e2}`);
  throw t3.code = "NotSupportedInExtensionHost", t3;
}
function je(e2) {
  var t3;
  const r2 = null !== (t3 = null == e2 ? void 0 : e2.timeoutMs) && void 0 !== t3 ? t3 : 1500, n2 = /* @__PURE__ */ new Map(), i2 = /* @__PURE__ */ new Set(), o2 = (e3) => {
    if (ke(t4 = e3.data) && "response" === t4.kind) {
      const t5 = n2.get(e3.data.requestId);
      if (!t5) return;
      return n2.delete(e3.data.requestId), clearTimeout(t5.timeoutId), void t5.resolve(e3.data);
    }
    var t4;
    (function(e4) {
      return ke(e4) && "event" === e4.kind;
    })(e3.data) && i2.forEach((t5) => {
      t5(e3.data);
    });
  };
  return window.addEventListener("message", o2), { request: (e3, t4, i3) => s(function* () {
    const o3 = (function(e4, t5) {
      return { source: $e, version: 1, kind: "request", requestId: _e(), payload: { method: e4, params: t5 } };
    })(e3, t4), s2 = yield new Promise((t5, s3) => {
      let l3;
      const a3 = i3 && "timeoutMs" in i3 ? i3.timeoutMs : r2;
      "number" == typeof a3 && a3 >= 0 && (l3 = setTimeout(() => {
        n2.delete(o3.requestId), s3(/* @__PURE__ */ new Error(`Extension host timeout while calling ${e3}`));
      }, a3)), n2.set(o3.requestId, { resolve: t5, reject: s3, timeoutId: l3 }), window.postMessage(o3, "*");
    });
    var l2, a2;
    if (!s2.payload.ok) throw new Error(null !== (l2 = null === (a2 = s2.payload.error) || void 0 === a2 ? void 0 : a2.message) && void 0 !== l2 ? l2 : `Extension host request failed: ${e3}`);
    return s2.payload.result;
  })(), onEvent: (e3) => (i2.add(e3), () => {
    i2.delete(e3);
  }), destroy() {
    window.removeEventListener("message", o2), n2.forEach((e3) => {
      clearTimeout(e3.timeoutId), e3.reject(/* @__PURE__ */ new Error("Extension bridge destroyed"));
    }), n2.clear(), i2.clear();
  } };
}
function Ne() {
  return (Ne = s(function* (e2) {
    const t3 = je();
    try {
      var r2;
      const i2 = yield t3.request("handshake", { debug: null !== (r2 = null == e2 ? void 0 : e2.debug) && void 0 !== r2 && r2 }, { timeoutMs: 5e3 });
      return n.log("Extension host probe response:", i2), true === i2.available;
    } catch (i2) {
      return n.log("Extension host probe failed:", i2), false;
    } finally {
      t3.destroy();
    }
  })).apply(this, arguments);
}
function We() {
  return We = s(function* () {
    const e2 = je(), t3 = /* @__PURE__ */ new Map();
    e2.onEvent((e3) => {
      const r3 = t3.get(e3.payload.event);
      null == r3 || r3.forEach((t4) => {
        t4(e3.payload.data);
      });
    }), yield e2.request("handshake", {}, { timeoutMs: 5e3 });
    const r2 = yield e2.request("getPlatformConfig", {}), n2 = { bus: { $on(r3, n3) {
      var i3;
      const o3 = null !== (i3 = t3.get(r3)) && void 0 !== i3 ? i3 : /* @__PURE__ */ new Set(), s2 = 0 === o3.size;
      o3.add(n3), t3.set(r3, o3), s2 && e2.request("subscribe", { event: r3 });
    }, $off(r3, n3) {
      const i3 = t3.get(r3);
      i3 && (i3.delete(n3), 0 === i3.size && (t3.delete(r3), e2.request("unsubscribe", { event: r3 })));
    }, $once(e3, t4) {
      const r3 = function() {
        n2.bus.$off(e3, r3), t4(...arguments);
      };
      n2.bus.$on(e3, r3);
    }, $emit() {
      Te("bus.$emit");
    } }, genReqId: () => _e(), query: (t4, r3) => e2.request("query", { params: t4, config: r3 }), createObservableQuery: (o2 = s(function* () {
      return Te("createObservableQuery");
    }), function() {
      return o2.apply(this, arguments);
    }), openApp: () => Te("openApp"), openAppSetting: () => Te("openAppSetting"), openCustomApp: () => Te("openCustomApp"), setTitle: () => Te("setTitle"), pickFile: (t4) => e2.request("pickFile", t4, { timeoutMs: null }), pickUserFile: () => Te("pickUserFile"), pickSharedFile: () => Te("pickSharedFile"), showFileDetails: (e3, t4) => Te("showFileDetails"), authorizeUserFile: (e3) => Te("authorizeUserFile"), authorizeSharedFile: (e3) => Te("authorizeSharedFile"), close: (i2 = s(function* () {
      e2.destroy();
    }), function() {
      return i2.apply(this, arguments);
    }), refreshToken: () => e2.request("refreshToken", {}), setExitPageTips: () => Te("setExitPageTips"), openFileManagerApp: () => Te("openFileManagerApp"), openFileManager: () => Te("openFileManager"), openFile: () => Te("openFile"), getPlatformConfig: () => Promise.resolve(r2), getHostSnapshot: () => e2.request("getHostSnapshot", {}) };
    var i2, o2, l2;
    return { methods: n2, config: (l2 = r2, { appName: "extension-host", os: { version: l2.systemVersion, theme: l2.theme, language: l2.language, format: { date: { date: l2.format.date, time: l2.format.time } } } }) };
  }), We.apply(this, arguments);
}
var Ce = null;
function qe() {
  return (qe = s(function* (e2) {
    const t3 = Fe({ callTimeouts: { getSessionCredentials: 5e3 }, debug: null == e2 ? void 0 : e2.debug, timeout: 1500 });
    try {
      const e3 = yield t3.promise;
      return e3;
    } catch (r2) {
      return t3.destroy(), n.log("Native OS connection probe failed:", r2), null;
    }
  })).apply(this, arguments);
}
var xe = (e2) => Ce || (Ce = s(function* () {
  if ("undefined" != typeof window && window.parent !== window) {
    const t3 = yield (function(e3) {
      return qe.apply(this, arguments);
    })(e2);
    if (t3) return t3;
  }
  return (yield (function(e3) {
    return Ne.apply(this, arguments);
  })(e2)) ? (function() {
    return We.apply(this, arguments);
  })() : Fe({ callTimeouts: { getSessionCredentials: 5e3 }, debug: null == e2 ? void 0 : e2.debug }).promise;
})());
var Ie = { pickFile: "pick-file", pickUserFile: "pick-user-file", pickSharedFile: "pick-shared-file", authorizeUserFile: "authorize-user-file", authorizeSharedFile: "authorize-shared-file" };
var Ue = class {
  constructor() {
    r(this, "options", void 0), r(this, "flutterInAppWebView", null), r(this, "osConnector", null), r(this, "initPromise", void 0), r(this, "appApiVersion", void 0), r(this, "isWeb", true), r(this, "isStandaloneWeb", false);
    let e2 = arguments.length > 0 && void 0 !== arguments[0] ? arguments[0] : {};
    this.options = e2, e2.debug && (n.setDebug(true), n.log("Trim App initialized with debug mode")), n.log("Options:", e2), this.initPromise = this.init();
  }
  init() {
    var e2 = this;
    return s(function* () {
      return n.log("Starting initialization..."), new Promise((t3) => {
        if (i()) e2.isWeb = false, n.log("Initializing mobile platform..."), new Promise((e3) => {
          var t4;
          (c ? window.flutter_inappwebview : u || (null === (t4 = window.flutter_inappwebview) || void 0 === t4 ? void 0 : t4._platformReady)) ? (n.log("flutter_inappwebview is ready"), e3(window.flutter_inappwebview)) : window.addEventListener("flutterInAppWebViewPlatformReady", () => {
            n.log("flutter_inappwebview is ready"), e3(window.flutter_inappwebview);
          });
        }).then((r2 = s(function* (r3) {
          yield e2.initMobileAppApi(r3), n.log("Mobile platform initialized"), t3();
        }), function(e3) {
          return r2.apply(this, arguments);
        }));
        else {
          if (e2.isWeb = true, e2.isStandaloneWeb = "undefined" != typeof window && window.parent === window, e2.isStandaloneWeb) return n.log("Standalone web platform detected; host bridge initialization skipped"), void t3();
          n.log("Initializing web platform..."), xe().then((r3) => {
            e2.osConnector = r3, n.log("Web platform initialized"), t3();
          });
        }
        var r2;
      });
    })();
  }
  getWebMethods() {
    var e2;
    const t3 = null === (e2 = this.osConnector) || void 0 === e2 ? void 0 : e2.methods;
    if (!t3) throw new Error("Host bridge is not available outside iframe or app runtime");
    return t3;
  }
  loadAppMessage() {
    var e2 = this;
    return s(function* () {
      return e2.callAppMethod("getAppMessage");
    })();
  }
  initMobileAppApi(e2) {
    var t3 = this;
    return s(function* () {
      t3.flutterInAppWebView = e2;
      try {
        const e3 = yield t3.loadAppMessage();
        t3.appApiVersion = null == e3 ? void 0 : e3.appApi;
      } catch (r2) {
        n.log("Failed to load mobile appApi version:", r2), t3.appApiVersion = void 0;
      }
    })();
  }
  assertMobileAppApi(e2) {
    let t3 = arguments.length > 1 && void 0 !== arguments[1] ? arguments[1] : "v1.0";
    if (!this.isAtLeastVersion(this.appApiVersion, t3)) throw new Error(`${e2} requires appApi >= ${t3}`);
  }
  isAtLeastVersion(e2, t3) {
    if (!e2) return false;
    const r2 = this.parseVersion(e2), n2 = this.parseVersion(t3), i2 = Math.max(r2.length, n2.length);
    for (let l2 = 0; l2 < i2; l2 += 1) {
      var o2, s2;
      const e3 = null !== (o2 = r2[l2]) && void 0 !== o2 ? o2 : 0, t4 = null !== (s2 = n2[l2]) && void 0 !== s2 ? s2 : 0;
      if (e3 > t4) return true;
      if (e3 < t4) return false;
    }
    return true;
  }
  parseVersion(e2) {
    return e2.replace(/^v/i, "").split(".").map((e3) => Number.parseInt(e3, 10) || 0);
  }
  callAppMethod(e2) {
    var t3 = arguments, r2 = this;
    return s(function* () {
      if (!r2.flutterInAppWebView) return null;
      for (var n2 = t3.length, i2 = new Array(n2 > 1 ? n2 - 1 : 0), o2 = 1; o2 < n2; o2++) i2[o2 - 1] = t3[o2];
      return p(r2.flutterInAppWebView, e2, ...i2);
    })();
  }
  ready() {
    var e2 = this;
    return s(function* () {
      return e2.initPromise;
    })();
  }
  getHostCapabilities() {
    var e2 = this;
    return s(function* () {
      var t3;
      let r2;
      try {
        yield Promise.race([e2.initPromise, new Promise((e3, t4) => {
          r2 = setTimeout(() => t4(/* @__PURE__ */ new Error("Host connection unavailable")), 5e3);
        })]);
      } finally {
        void 0 !== r2 && clearTimeout(r2);
      }
      if (!e2.isWeb) return { runtime: "mobile" };
      const n2 = null === (t3 = e2.osConnector) || void 0 === t3 || null === (t3 = t3.config) || void 0 === t3 ? void 0 : t3.host;
      return "desktop" === (null == n2 ? void 0 : n2.runtime) ? a({ runtime: "desktop" }, 1 === n2.sessionCredentials ? { sessionCredentials: 1 } : {}) : { runtime: "web" === (null == n2 ? void 0 : n2.runtime) ? "web" : "unknown" };
    })();
  }
  getSessionCredentials() {
    var e2 = this;
    return s(function* () {
      const t3 = (e3) => ({ code: 1, msg: e3, error: e3, data: null });
      try {
        var r2, n2;
        const i2 = yield e2.getHostCapabilities(), o2 = null === (r2 = e2.osConnector) || void 0 === r2 ? void 0 : r2.methods.getSessionCredentials;
        if ("desktop" !== i2.runtime || 1 !== i2.sessionCredentials || !o2) return t3("NOT_SUPPORTED");
        const s2 = yield o2();
        return 0 === (null == s2 ? void 0 : s2.code) && "string" == typeof (null === (n2 = s2.data) || void 0 === n2 ? void 0 : n2.secret) && s2.data.secret ? { code: 0, msg: "", data: a({ secret: s2.data.secret }, "string" == typeof s2.data.token && s2.data.token ? { token: s2.data.token } : {}) } : 1 === (null == s2 ? void 0 : s2.code) && ["NOT_SUPPORTED", "FORBIDDEN", "SESSION_UNAVAILABLE", "CONNECTION_CLOSED"].includes(s2.error) ? t3(s2.error) : t3("SESSION_UNAVAILABLE");
      } catch (i2) {
        return t3("CONNECTION_CLOSED");
      }
    })();
  }
  getOptions() {
    return this.options;
  }
  setOptions(e2) {
    this.options = a(a({}, this.options), e2), void 0 !== e2.debug && n.setDebug(e2.debug), n.log("Options updated:", this.options);
  }
  getPlatformConfig() {
    var e2 = this;
    return s(function* () {
      if (yield e2.initPromise, e2.isWeb) {
        const t4 = yield e2.getWebMethods().getPlatformConfig();
        if (!t4) throw new Error("Failed to get platform config");
        return t4;
      }
      if (e2.appApiVersion) {
        const t4 = yield e2.callAppMethod("getPlatformConfig");
        if (!t4) throw new Error("Failed to get platform config");
        return t4;
      }
      const t3 = yield e2.loadAppMessage();
      if (!t3) throw new Error("Failed to get platform config");
      return e2.appApiVersion = t3.appApi, { theme: t3.nightMode, language: t3.language, appVersion: t3.appVersion, systemVersion: t3.systemVersion, format: { date: t3.dateFormat, time: t3.timeFormat } };
    })();
  }
  getHostSnapshot() {
    var e2 = this;
    return s(function* () {
      if (yield e2.initPromise, !e2.isWeb) throw new Error("getHostSnapshot is not supported on mobile platform");
      const t3 = yield e2.getWebMethods().getHostSnapshot();
      if (!t3) throw new Error("Failed to get host snapshot");
      return t3;
    })();
  }
  getAppAuthBaseUrl() {
    var e2 = this;
    return s(function* () {
      if ("undefined" != typeof window && window.parent !== window) {
        const e3 = document.referrer;
        if (e3) try {
          return new URL(e3).origin;
        } catch (t3) {
          n.log("Failed to derive app auth base url from document.referrer:", t3);
        }
      }
      if ("undefined" != typeof window) return window.location.origin;
      if (yield e2.initPromise, e2.isWeb) try {
        const t3 = yield e2.getHostSnapshot();
        if (t3.host) return t3.host;
      } catch (t3) {
        n.log("Failed to derive app auth host snapshot:", t3);
      }
      throw new Error("Failed to derive app auth base url");
    })();
  }
  buildAppAuthUrl(e2, t3) {
    var r2 = this;
    return s(function* () {
      return (function(e3, t4, r3, n2) {
        var i2, o2;
        const s2 = new URL(e3, null !== (i2 = null != n2 ? n2 : null === (o2 = globalThis.location) || void 0 === o2 ? void 0 : o2.origin) && void 0 !== i2 ? i2 : "http://localhost"), l2 = Ie[t4];
        if (s2.pathname = (function(e4) {
          return `/app-auth/${e4}`;
        })(l2), s2.searchParams.set("appName", r3.appName), r3.redirectUri && s2.searchParams.set("redirectUri", r3.redirectUri), r3.state && s2.searchParams.set("state", r3.state), "pickFile" === t4 || "pickUserFile" === t4 || "pickSharedFile" === t4) {
          var a2;
          const e4 = r3;
          "pickSharedFile" !== t4 && e4.directory && s2.searchParams.set("directory", "true");
          const n3 = "pickSharedFile" !== t4 ? e4.accept : void 0;
          (null == n3 ? void 0 : n3.length) && s2.searchParams.set("accept", n3.join(",")), (null === (a2 = e4.sidebarGroup) || void 0 === a2 ? void 0 : a2.length) && s2.searchParams.set("sidebarGroup", e4.sidebarGroup.join(","));
        } else s2.searchParams.set("path", r3.path);
        return s2.toString();
      })(yield r2.getAppAuthBaseUrl(), e2, t3, window.location.origin);
    })();
  }
  openAppAuth(e2, t3, r2) {
    var n2 = this;
    return s(function* () {
      if (yield n2.initPromise, !n2.isWeb) throw new Error("openAppAuth is not supported on mobile platform");
      const i2 = yield n2.buildAppAuthUrl(e2, t3);
      return "_self" === (null == r2 ? void 0 : r2.target) && "undefined" != typeof window ? (window.location.assign(i2), i2) : (yield n2.openURL(i2, null == r2 ? void 0 : r2.target, null == r2 ? void 0 : r2.features), i2);
    })();
  }
  parseAppAuthCallback(e2) {
    return (function(e3) {
      var t3, r2, n2, i2;
      const o2 = e3 instanceof URLSearchParams ? e3 : e3 instanceof URL ? e3.searchParams : "string" == typeof e3 ? new URL(e3, null !== (t3 = null === (r2 = globalThis.location) || void 0 === r2 ? void 0 : r2.origin) && void 0 !== t3 ? t3 : "http://localhost").searchParams : new URLSearchParams(null !== (n2 = null === (i2 = globalThis.location) || void 0 === i2 ? void 0 : i2.search) && void 0 !== n2 ? n2 : ""), s2 = o2.get("method"), l2 = s2 && Object.prototype.hasOwnProperty.call(Ie, s2) ? s2 : void 0, a2 = o2.get("path");
      let u2;
      if (a2) try {
        const e4 = JSON.parse(a2);
        Array.isArray(e4) && e4.every((e5) => "string" == typeof e5) && (u2 = e4);
      } catch (c2) {
        u2 = void 0;
      }
      return { status: o2.get("status") || void 0, error: o2.get("error") || void 0, method: l2, appName: o2.get("appName") || void 0, state: o2.get("state") || void 0, path: u2 };
    })(e2);
  }
  setTitle(e2) {
    var t3 = this;
    return s(function* () {
      if (yield t3.initPromise, n.log("Setting title:", e2), document.title = e2, t3.isWeb) return t3.getWebMethods().setTitle(e2);
    })();
  }
  setExitPageTips(e2) {
    var t3 = this;
    return s(function* () {
      return yield t3.initPromise, t3.isWeb ? t3.getWebMethods().setExitPageTips(e2) : t3.callAppMethod("setExitPageTips", e2 ? JSON.stringify(e2) : "");
    })();
  }
  openFile(e2) {
    var t3 = this;
    return s(function* () {
      return yield t3.initPromise, n.log("Opening file:", e2), t3.isWeb ? t3.getWebMethods().openFile(e2) : t3.callAppMethod("openFile", e2);
    })();
  }
  openFileManager(e2) {
    var t3 = this;
    return s(function* () {
      if (yield t3.initPromise, n.log("Opening file manager:", e2), t3.isWeb) {
        const r2 = t3.getWebMethods();
        return (r2.openFileManager || r2.openFileManagerApp)(e2);
      }
      return t3.assertMobileAppApi("openFileManager"), t3.callAppMethod("openFileManager", e2);
    })();
  }
  showFileDetails(e2, t3) {
    var r2 = this;
    return s(function* () {
      return yield r2.initPromise, r2.isWeb ? r2.getWebMethods().showFileDetails(e2, t3) : (r2.assertMobileAppApi("showFileDetails"), r2.callAppMethod("showFileDetails", JSON.stringify({ paths: e2, options: t3 })));
    })();
  }
  authorizeUserFile(e2) {
    var t3 = this;
    return s(function* () {
      var r2;
      return yield t3.initPromise, t3.isWeb ? t3.getWebMethods().authorizeUserFile(e2) : (t3.assertMobileAppApi("authorizeUserFile"), null !== (r2 = yield t3.callAppMethod("authorizeUserFile", JSON.stringify({ path: e2 }))) && void 0 !== r2 ? r2 : void 0);
    })();
  }
  authorizeSharedFile(e2) {
    var t3 = this;
    return s(function* () {
      var r2;
      return yield t3.initPromise, t3.isWeb ? t3.getWebMethods().authorizeSharedFile(e2) : (t3.assertMobileAppApi("authorizeSharedFile"), null !== (r2 = yield t3.callAppMethod("authorizeSharedFile", JSON.stringify({ path: e2 }))) && void 0 !== r2 ? r2 : void 0);
    })();
  }
  openAppSetting() {
    var e2 = this;
    return s(function* () {
      return yield e2.initPromise, n.log("Opening app setting"), e2.isWeb ? e2.getWebMethods().openAppSetting() : (e2.assertMobileAppApi("openAppSetting"), e2.callAppMethod("openAppSetting"));
    })();
  }
  openApp(e2) {
    var t3 = this;
    return s(function* () {
      return yield t3.initPromise, n.log("Opening app:", e2), t3.isWeb ? t3.getWebMethods().openApp(e2) : !!i() && t3.callAppMethod("openAppPage", e2);
    })();
  }
  openCustomApp(e2, t3) {
    var r2 = this;
    return s(function* () {
      if (yield r2.initPromise, n.log("Opening custom app:", e2, t3), r2.isWeb) return r2.getWebMethods().openCustomApp(e2, t3);
      throw new Error("openCustomApp is not supported on mobile platform");
    })();
  }
  openURL(e2, t3, r2) {
    var i2 = this;
    return s(function* () {
      if (yield i2.initPromise, n.log("Opening URL:", e2, t3, r2), !i2.isWeb) return i2.callAppMethod("openSystemBrowser", e2);
      if ("_self" === t3) return void window.location.assign(e2);
      if (!r2) return void window.open(e2, t3, r2);
      const o2 = window.open("", t3, r2);
      o2 && (o2.location.href = e2);
    })();
  }
  close() {
    var e2 = this;
    return s(function* () {
      return yield e2.initPromise, e2.isWeb ? e2.getWebMethods().close() : e2.callAppMethod("exitPage");
    })();
  }
  query(e2, t3) {
    var r2 = this;
    return s(function* () {
      if (yield r2.initPromise, r2.isWeb) {
        if (null == t3 ? void 0 : t3.observable) return r2.getWebMethods().createObservableQuery(e2, t3);
        const n3 = yield r2.getWebMethods().query(e2, t3);
        if (!n3) throw new Error("Failed to query");
        return n3;
      }
      if (r2.assertMobileAppApi("query"), null == t3 ? void 0 : t3.observable) {
        if (!r2.flutterInAppWebView) throw new Error("Failed to start observable query");
        return (function(e3, t4, r3) {
          return v(), new f((n3) => {
            let i2, o2 = false;
            return p(e3, "wsQuery", null == t4 ? void 0 : t4.req, JSON.stringify(t4), void 0 === r3 ? void 0 : JSON.stringify(r3)).then((e4) => {
              (null == e4 ? void 0 : e4.reqId) ? (i2 = e4.reqId, o2 || h.set(i2, n3)) : n3.error(/* @__PURE__ */ new Error("Failed to start observable query"));
            }, (e4) => {
              n3.error(e4);
            }), () => {
              o2 = true, i2 && h.delete(i2);
            };
          });
        })(r2.flutterInAppWebView, e2, t3);
      }
      const n2 = yield r2.callAppMethod("wsQuery", null == e2 ? void 0 : e2.req, JSON.stringify(e2), void 0 === t3 ? void 0 : JSON.stringify(t3));
      if (!n2) throw new Error("Failed to query");
      if ("succ" !== n2.result) throw n2;
      return n2;
    })();
  }
  refreshToken() {
    var e2 = this;
    return s(function* () {
      if (yield e2.initPromise, !e2.isWeb) throw new Error("refreshToken is not supported on mobile platform");
      return e2.getWebMethods().refreshToken();
    })();
  }
  $on(e2, t3) {
    var r2 = this;
    return s(function* () {
      if (yield r2.initPromise, !r2.isWeb) throw new Error("$on is not supported on mobile platform");
      const n2 = r2.getWebMethods().bus;
      if (!n2) throw new Error("bus is not available");
      n2.$on(e2, t3);
    })();
  }
  $off(e2, t3) {
    var r2 = this;
    return s(function* () {
      if (yield r2.initPromise, !r2.isWeb) throw new Error("$off is not supported on mobile platform");
      const n2 = r2.getWebMethods().bus;
      if (!n2) throw new Error("bus is not available");
      n2.$off(e2, t3);
    })();
  }
  $once(e2, t3) {
    var r2 = this;
    return s(function* () {
      if (yield r2.initPromise, !r2.isWeb) throw new Error("$once is not supported on mobile platform");
      const n2 = r2.getWebMethods().bus;
      if (!n2) throw new Error("bus is not available");
      n2.$once(e2, t3);
    })();
  }
  pickFile(e2) {
    var t3 = this;
    return s(function* () {
      var r2, n2;
      return yield t3.initPromise, t3.isWeb ? null === (r2 = t3.osConnector) || void 0 === r2 ? void 0 : r2.methods.pickFile(e2) : t3.isAtLeastVersion(t3.appApiVersion, "v1.0") ? null !== (n2 = yield t3.callAppMethod("pickFile", JSON.stringify(e2))) && void 0 !== n2 ? n2 : void 0 : t3.flutterInAppWebView ? (function(e3, t4) {
        return y.apply(this, arguments);
      })(t3.flutterInAppWebView, e2) : void 0;
    })();
  }
  pickUserFile() {
    var e2 = arguments, t3 = this;
    return s(function* () {
      var r2;
      let n2 = e2.length > 0 && void 0 !== e2[0] ? e2[0] : {};
      return yield t3.initPromise, t3.isWeb ? t3.getWebMethods().pickUserFile(n2) : (t3.assertMobileAppApi("pickUserFile"), null !== (r2 = yield t3.callAppMethod("pickUserFile", JSON.stringify(n2))) && void 0 !== r2 ? r2 : void 0);
    })();
  }
  pickSharedFile() {
    var e2 = arguments, t3 = this;
    return s(function* () {
      var r2;
      let n2 = e2.length > 0 && void 0 !== e2[0] ? e2[0] : {};
      return yield t3.initPromise, t3.isWeb ? t3.getWebMethods().pickSharedFile(n2) : (t3.assertMobileAppApi("pickSharedFile"), null !== (r2 = yield t3.callAppMethod("pickSharedFile", JSON.stringify(n2))) && void 0 !== r2 ? r2 : void 0);
    })();
  }
};

// src/client/env.ts
function isFnosApp(userAgent) {
  const ua = userAgent ?? (typeof navigator !== "undefined" ? navigator.userAgent || "" : "");
  return /FNAppType\//.test(ua);
}
function isConsoleEmbeddedDsh(pathname) {
  const path = pathname ?? (typeof window !== "undefined" ? window.location.pathname : "");
  return /\/gui(?:\/|$)/.test(path);
}
function isFnosAppEmbedded() {
  return isFnosApp() && isConsoleEmbeddedDsh();
}

// src/client/sdk.ts
var sdkInstance = null;
function getSdk() {
  if (!sdkInstance) sdkInstance = new Ue();
  return sdkInstance;
}
function isFnosHost() {
  if (isConsoleEmbeddedDsh()) return false;
  const sdk = getSdk();
  if (sdk.isWeb && sdk.isStandaloneWeb) return false;
  return window.parent !== window || sdk.isWeb === false;
}
function isHostLikeWindow() {
  const sdk = getSdk();
  return window.parent !== window || sdk.isWeb === false;
}

// src/client/auth.ts
async function buildAuthUrl(base, state, method, params, appName) {
  const sdk = getSdk();
  const previousBase = sdk.getAppAuthBaseUrl;
  sdk.getAppAuthBaseUrl = async () => base;
  try {
    const payload = {
      appName,
      ...params,
      redirectUri: pluginURL("fnos-advanced/auth-callback.html"),
      state
    };
    const build = sdk.buildAppAuthUrl;
    const url = await build.call(sdk, method, payload);
    const target = new URL(url);
    if (payload.creatable) target.searchParams.set("creatable", "true");
    return target.toString();
  } finally {
    sdk.getAppAuthBaseUrl = previousBase;
  }
}
function openAuthPopup() {
  const popup = window.open("", `dsh-fnos-advanced-auth-${randomId()}`, POPUP_FEATURES);
  if (popup) writePopupLoading(popup);
  return popup;
}
function windowSurface(win) {
  return {
    get closed() {
      return win.closed;
    },
    navigate(url) {
      win.location.replace(url);
    }
  };
}
function openAuthFrame() {
  if (typeof document === "undefined" || !document.body) return null;
  let closed = false;
  const overlay = document.createElement("div");
  overlay.setAttribute("data-dsh-fnos-auth-frame", "");
  overlay.style.cssText = [
    "position:fixed",
    "inset:0",
    "z-index:2147483000",
    "display:flex",
    "flex-direction:column",
    "box-sizing:border-box",
    "background:#fff",
    "color:#1f2328",
    "overscroll-behavior:contain",
    // Keep the bar/iframe clear of the iOS notch and home indicator.
    "padding:env(safe-area-inset-top) env(safe-area-inset-right) env(safe-area-inset-bottom) env(safe-area-inset-left)"
  ].join(";");
  const bar = document.createElement("div");
  bar.style.cssText = 'flex:0 0 auto;display:flex;align-items:center;justify-content:space-between;gap:12px;padding:8px 12px;border-bottom:1px solid rgba(127,127,127,.28);font:13px/1.4 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif';
  const title = document.createElement("span");
  title.textContent = t("飞牛授权");
  const cancel = document.createElement("button");
  cancel.type = "button";
  cancel.textContent = t("取消");
  cancel.style.cssText = "flex:0 0 auto;border:1px solid rgba(127,127,127,.4);border-radius:8px;background:transparent;color:inherit;font:inherit;padding:4px 12px;cursor:pointer";
  bar.append(title, cancel);
  const frame = document.createElement("iframe");
  frame.title = t("飞牛授权");
  frame.setAttribute("allow", "clipboard-read; clipboard-write");
  frame.style.cssText = "flex:1 1 auto;width:100%;border:0;background:#fff";
  const loading = t("正在打开飞牛授权页面…").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  frame.srcdoc = `<!doctype html><meta charset="utf-8"><style>html,body{height:100%;margin:0;display:flex;align-items:center;justify-content:center;font:14px/1.6 -apple-system,BlinkMacSystemFont,"PingFang SC","Segoe UI",system-ui,sans-serif;color:#6a6f76;background:#f6f7f9}</style><p>${loading}</p>`;
  frame.addEventListener("load", () => {
    try {
      frame.contentWindow?.focus();
    } catch {
    }
  });
  overlay.append(bar, frame);
  function close() {
    if (closed) return;
    closed = true;
    document.removeEventListener("keydown", onKeydown);
    overlay.remove();
  }
  function onKeydown(event) {
    if (event.key === "Escape") close();
  }
  cancel.addEventListener("click", close);
  for (const type of ["pointerdown", "mousedown", "click", "touchstart"]) {
    overlay.addEventListener(type, (event) => event.stopPropagation());
  }
  document.addEventListener("keydown", onKeydown);
  document.body.appendChild(overlay);
  return {
    surface: {
      get closed() {
        return closed;
      },
      navigate(url) {
        if (closed) return;
        frame.removeAttribute("srcdoc");
        frame.src = url;
      }
    },
    close
  };
}
function readStoredResult() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    return JSON.parse(raw);
  } catch {
    return null;
  }
}
function acceptStoredResult(expectedState, accept) {
  const stored = readStoredResult();
  if (!stored) return;
  if (expectedState && stored.state && stored.state !== expectedState) return;
  accept(stored);
}
function parseAuthPayload(payload) {
  if (!payload) return null;
  if (typeof payload.search === "string" && payload.search) {
    try {
      const parsed = getSdk().parseAppAuthCallback(payload.search);
      if (parsed && (parsed.status || parsed.error || parsed.path && parsed.path.length)) {
        return parsed;
      }
    } catch {
    }
  }
  return payload.result || null;
}
function requestAuth({ surface, state, buildUrl }) {
  return new Promise((resolve, reject) => {
    let settled = false;
    let closeTimer = 0;
    let pollTimer = 0;
    let resultPollTimer = 0;
    let resultPolling = false;
    let timeoutTimer = 0;
    const lookupStored = () => acceptStoredResult(state, accept);
    const cleanup = () => {
      window.removeEventListener("message", onMessage);
      window.removeEventListener("storage", onStorage);
      window.removeEventListener("focus", lookupStored);
      document.removeEventListener("visibilitychange", onVisibility);
      window.clearInterval(pollTimer);
      window.clearInterval(resultPollTimer);
      window.clearTimeout(closeTimer);
      window.clearTimeout(timeoutTimer);
      try {
        localStorage.removeItem(STORAGE_KEY);
      } catch {
      }
    };
    const finish = (result) => {
      if (settled) return;
      settled = true;
      cleanup();
      resolve(result);
    };
    const fail = (error) => {
      if (settled) return;
      settled = true;
      cleanup();
      reject(error);
    };
    function accept(payload) {
      if (!payload || settled) return;
      const payloadState = payload.state || payload.result && payload.result.state || "";
      if (state && payloadState && payloadState !== state) return;
      const result = parseAuthPayload(payload);
      if (result) finish(result);
    }
    function onMessage(event) {
      if (event.origin !== window.location.origin) return;
      const data = event.data;
      if (!data || data.type !== MSG_TYPE) return;
      accept(data);
    }
    function onStorage(event) {
      if (event.key !== STORAGE_KEY || !event.newValue) return;
      try {
        accept(JSON.parse(event.newValue));
      } catch {
      }
    }
    function onVisibility() {
      if (document.visibilityState === "visible") lookupStored();
    }
    async function readServerResult() {
      try {
        const response = await fetch(
          `${pluginURL("fnos-advanced/auth-result")}?state=${encodeURIComponent(state)}`,
          { credentials: "same-origin", cache: "no-store", headers: { Accept: "application/json" } }
        );
        if (!response.ok) return;
        const data = await response.json();
        if (data && data.ok && typeof data.search === "string") {
          accept({ state: data.state || state, search: data.search });
        }
      } catch {
      }
    }
    async function pollServerResult() {
      if (settled || resultPolling) return;
      resultPolling = true;
      try {
        await readServerResult();
      } finally {
        resultPolling = false;
      }
    }
    window.addEventListener("message", onMessage);
    window.addEventListener("storage", onStorage);
    window.addEventListener("focus", lookupStored);
    document.addEventListener("visibilitychange", onVisibility);
    acceptStoredResult(state, accept);
    resultPollTimer = window.setInterval(pollServerResult, 900);
    pollTimer = window.setInterval(() => {
      if (!surface.closed) return;
      window.clearInterval(pollTimer);
      closeTimer = window.setTimeout(async () => {
        const stored = readStoredResult();
        if (stored) {
          accept(stored);
          return;
        }
        await readServerResult();
        if (!settled) finish({ status: "cancelled", error: t("授权窗口已关闭") });
      }, 450);
    }, 500);
    timeoutTimer = window.setTimeout(() => {
      fail(new Error(t("授权超时，请重试")));
    }, AUTH_TIMEOUT_MS);
    void (async () => {
      try {
        const url = await buildUrl();
        if (settled) return;
        surface.navigate(url);
      } catch (error) {
        fail(error instanceof Error ? error : new Error(String(error)));
      }
    })();
  });
}

// src/client/picker.ts
var SIDEBAR_GROUPS = Object.freeze([
  "myFiles",
  "otherShare",
  "external",
  "remote",
  "favorites",
  "team"
]);
function pickerParams(flow) {
  const params = {
    title: flow.title,
    okText: flow.okText,
    sidebarGroup: [...SIDEBAR_GROUPS]
  };
  if (flow.directory !== void 0) params.directory = flow.directory === true;
  if (flow.creatable) params.creatable = true;
  if (flow.multiple !== void 0) params.multiple = flow.multiple === true;
  return params;
}
function normalizeWorkspacePath(value) {
  const text = String(value ?? "");
  if (!text) return "";
  return text.replace(/\/+/g, "/").replace(/\/+$/, "") || "/";
}
function isDuplicateWorkspacePath(candidate, workspacePaths2) {
  const target = normalizeWorkspacePath(candidate);
  if (!target || !Array.isArray(workspacePaths2)) return false;
  return workspacePaths2.some((path) => normalizeWorkspacePath(path) === target);
}

// src/client/picker-flow.ts
function usesWebPicker(flow) {
  return isConsoleEmbeddedDsh() || isFnosApp() && flow.directory === true;
}
function usesAuthFrame() {
  return isFnosApp();
}
async function runPickFlow(button, flow) {
  if (button.dataset.busy === "1") return;
  markBusy(button, flow);
  const webPicker = usesWebPicker(flow);
  const useFrame = usesAuthFrame();
  let popup = null;
  if (!useFrame && (webPicker || !isHostLikeWindow())) {
    popup = window.open("", `dsh-fnos-advanced-auth-${randomId()}`, POPUP_FEATURES);
    if (popup) writePopupLoading(popup);
  }
  try {
    const direct = await pickPathsDirect(flow);
    if (direct.handled) {
      closePopup(popup);
      if (direct.paths && direct.paths.length) {
        await finishPick(flow, direct.paths);
      } else if (direct.cancelled) {
        flow.status(t("已取消选择"), false);
        flow.restore();
      } else {
        flow.status(translateError(direct.error) || t("打开选择器失败"), true);
        flow.restore();
      }
      return;
    }
    const config = await loadConfig();
    const appName = config.appName || "Harness";
    const settings = await loadSettings();
    const target = await resolveAuthTargets(config, settings);
    if (target.mode === "choose") {
      closePopup(popup);
      showAuthChooser(flow, appName, target.choices);
      return;
    }
    if (!useFrame) {
      if (!popup || popup.closed) {
        popup = window.open("", `dsh-fnos-advanced-auth-${randomId()}`, POPUP_FEATURES);
      }
      if (!popup) throw new Error(t("浏览器拦截了授权窗口，请允许弹窗后重试。"));
    }
    await startAuthFlow(flow, appName, target.base, popup);
  } catch (error) {
    closePopup(popup);
    handlePickError(flow, error);
  }
}
function markBusy(button, flow) {
  button.dataset.busy = "1";
  button.disabled = true;
  flow.setLabel(t("准备授权…"));
}
async function finishPick(flow, paths) {
  if (flow.isDuplicate && await flow.isDuplicate(paths)) {
    flow.status(t("该目录已添加为工作区"), false);
    flow.restore();
    return;
  }
  await flow.onPaths(paths);
  flow.status(flow.doneText(paths), false);
  const label = flow.doneLabel();
  if (label) flow.button.textContent = label;
  window.setTimeout(() => flow.restore(), 1600);
}
async function pickPathsDirect(flow) {
  if (usesWebPicker(flow)) return { handled: false };
  const sdk = getSdk();
  try {
    await sdk.ready();
  } catch {
    return { handled: false };
  }
  if (sdk.isWeb && sdk.isStandaloneWeb) return { handled: false };
  if (sdk.isWeb && window.parent === window) return { handled: false };
  const picker = sdk[flow.method];
  if (typeof picker !== "function") return { handled: false };
  flow.setLabel(t("等待选择…"));
  let result;
  try {
    result = await picker.call(sdk, pickerParams(flow));
  } catch (error) {
    console.warn(LOG_PREFIX, "in-host picker unavailable; falling back to URL auth", error);
    flow.status(t("当前宿主未提供内嵌选择器，已改用网页授权。"), false);
    return { handled: false };
  }
  const paths = result && result.code === 0 && Array.isArray(result.data) ? result.data.filter((item) => typeof item === "string" && item) : [];
  if (paths.length) return { handled: true, paths };
  return {
    handled: true,
    cancelled: isPickerCancelled(result),
    error: result && result.msg ? result.msg : t("打开选择器失败")
  };
}
async function startAuthFlow(flow, appName, base, existingPopup) {
  flow.setLabel(t("等待授权…"));
  const state = randomId();
  const useFrame = usesAuthFrame();
  let frame = null;
  let popup = existingPopup;
  let surface;
  if (useFrame) {
    frame = openAuthFrame();
    if (!frame) throw new Error(t("无法打开授权页面，请重试。"));
    surface = frame.surface;
  } else {
    if (!popup || popup.closed) {
      popup = window.open("", `dsh-fnos-advanced-auth-${state}`, POPUP_FEATURES);
    }
    if (!popup) throw new Error(t("浏览器拦截了授权窗口，请允许弹窗后重试。"));
    writePopupLoading(popup);
    surface = windowSurface(popup);
  }
  try {
    const auth = await requestAuth({
      surface,
      state,
      buildUrl: () => buildAuthUrl(base, state, flow.method, { ...pickerParams(flow) }, appName)
    });
    if (!auth || auth.status !== "success") {
      if (isAuthCancelled(auth)) {
        if (auth && auth.status !== "cancel" && auth.status !== "cancelled") {
          console.info(LOG_PREFIX, "authorization result treated as cancellation", auth);
        }
        flow.status(t("已取消选择"), false);
        flow.restore();
        return;
      }
      throw new Error(auth && auth.error ? translateError(auth.error) : t("打开选择器失败"));
    }
    const paths = Array.isArray(auth.path) ? auth.path.filter((item) => typeof item === "string" && item) : [];
    if (!paths.length) throw new Error(t("授权结果里没有路径"));
    await finishPick(flow, paths);
  } catch (error) {
    closePopup(popup);
    throw error;
  } finally {
    frame?.close();
  }
}
function handlePickError(flow, error) {
  const message = error instanceof Error ? error.message : String(error);
  console.warn(LOG_PREFIX, message, error);
  flow.status(translateError(message), true);
  flow.restore();
}
function showAuthChooser(flow, appName, choices) {
  const anchor = flow.anchor;
  if (!anchor) {
    flow.restore();
    return;
  }
  removeChooser(anchor);
  const host = anchor;
  if (getComputedStyle(host).position === "static") host.style.position = "relative";
  flow.setLabel(t("选择授权入口…"));
  const panel = document.createElement("div");
  panel.className = "dsh-fnos-advanced-chooser";
  panel.setAttribute("role", "menu");
  panel.setAttribute("aria-label", t("选择授权入口"));
  const title = document.createElement("div");
  title.className = "dsh-fnos-advanced-chooser-title";
  title.textContent = t("选择授权入口");
  panel.appendChild(title);
  for (const choice of choices) {
    const option = document.createElement("button");
    option.type = "button";
    option.className = "dsh-fnos-advanced-chooser-option";
    option.setAttribute("role", "menuitem");
    const label = document.createElement("span");
    label.className = "dsh-fnos-advanced-chooser-label";
    label.textContent = choice.label;
    const detail = document.createElement("span");
    detail.className = "dsh-fnos-advanced-chooser-detail";
    detail.textContent = choice.detail;
    option.append(label, detail);
    option.addEventListener("click", (event) => {
      event.preventDefault();
      event.stopPropagation();
      removeChooser(anchor);
      startAuthFlow(flow, appName, choice.base, null).catch((error) => {
        handlePickError(flow, error);
      });
    });
    panel.appendChild(option);
  }
  const cancel = document.createElement("button");
  cancel.type = "button";
  cancel.className = "dsh-fnos-advanced-chooser-cancel";
  cancel.textContent = t("取消");
  cancel.addEventListener("click", (event) => {
    event.preventDefault();
    event.stopPropagation();
    removeChooser(anchor);
    flow.restore();
  });
  panel.appendChild(cancel);
  anchor.appendChild(panel);
}
function removeChooser(anchor) {
  anchor.querySelectorAll(".dsh-fnos-advanced-chooser").forEach((node) => node.remove());
}

// src/client/composer-menu.ts
function scanCommandMenu() {
  for (const menu of document.querySelectorAll("[data-trigger-menu]")) {
    if (!isVisible(menu)) continue;
    if (!isComposerCommandMenu(menu)) continue;
    const options = Array.from(menu.querySelectorAll('button[role="option"]'));
    const fileOption = options.find((option) => /\bfile\b/i.test(option.textContent || ""));
    if (!fileOption || !fileOption.parentElement) continue;
    if (menu.querySelector(`[${PICK_ITEM_ATTR}]`)) continue;
    const row = buildPickItem(options, fileOption, {
      attr: PICK_ITEM_ATTR,
      label: t("从飞牛选择"),
      alias: "fnos",
      description: t("选择 NAS 上的文件，非图片作为路径引用")
    });
    if (row) fileOption.after(row);
  }
}
function isComposerCommandMenu(menu) {
  const options = Array.from(menu.querySelectorAll('button[role="option"]'));
  return looksLikeComposerCommandMenu(options.map((option) => option.textContent || ""));
}
function buildPickItem(options, fileOption, entry) {
  const item = fileOption.cloneNode(true);
  item.removeAttribute("id");
  item.removeAttribute("aria-selected");
  item.removeAttribute("aria-label");
  item.setAttribute(entry.attr, "1");
  if (item.className) {
    item.className = item.className.split(/\s+/).filter((name2) => name2 && !/active/i.test(name2)).join(" ");
  }
  const spans = Array.from(item.querySelectorAll("span"));
  const icon = spans.find((span) => span.querySelector("svg")) || spans[0];
  const name = icon ? icon.nextElementSibling : spans[1];
  let alias = name ? name.nextElementSibling : null;
  if (alias && (alias.querySelector("svg") || !(alias.textContent || "").trim())) alias = null;
  let description = alias ? alias.nextElementSibling : name ? name.nextElementSibling : null;
  if (!description) {
    const template = options.map((option) => {
      const optionAlias = Array.from(option.querySelectorAll("span")).find((span) => (span.textContent || "").trim().toLowerCase() === "goal");
      return optionAlias ? optionAlias.nextElementSibling : null;
    }).find((node) => node instanceof HTMLElement && node.tagName === "SPAN");
    if (template) {
      description = template.cloneNode(false);
      item.appendChild(description);
    }
  }
  if (icon) icon.innerHTML = PICK_FILE_SVG;
  if (name) name.textContent = entry.label;
  if (alias) alias.textContent = entry.alias;
  if (description) description.textContent = entry.description;
  item.addEventListener("mousedown", (event) => {
    event.preventDefault();
    event.stopPropagation();
  });
  item.addEventListener("click", (event) => {
    event.preventDefault();
    event.stopPropagation();
    dismissTriggerMenu();
    void handlePickFiles(item);
  });
  return item;
}
async function handlePickFiles(button) {
  const active = findActiveComposerInput();
  if (!active) {
    showToast(t("没有找到对话的文件输入框，当前 dsh 版本可能不支持。"));
    return;
  }
  const { input, region } = active;
  if (input.disabled) {
    showToast(t("当前会话暂时不能添加附件。"));
    return;
  }
  const editor = region.querySelector("[data-composer-input]");
  if (editor && editor.getAttribute("contenteditable") === "false") {
    showToast(t("当前会话暂时不能添加附件。"));
    return;
  }
  await runPickFlow(button, composerFlow(input, button));
}
function composerFlow(input, button) {
  return {
    button,
    method: "pickUserFile",
    directory: false,
    multiple: true,
    title: t("选择文件"),
    okText: t("添加引用"),
    anchor: input.closest("[data-composer-card]") || input.parentElement,
    status: (text) => showToast(text),
    onPaths: (paths) => addPathsToComposer(paths, input),
    doneText: (paths) => t("已添加 {count} 个文件", { count: paths.length }),
    setLabel: () => {
    },
    doneLabel: () => "",
    restore: () => {
      button.disabled = false;
      delete button.dataset.busy;
    }
  };
}

// src/client/file-actions.ts
function fileRowActions(entryType, host) {
  if (entryType !== "file" && entryType !== "directory") return [];
  const actions = [];
  if (host) actions.push("open", "details");
  if (entryType === "file") actions.push("quote");
  actions.push("copy");
  return actions;
}

// src/client/path-menu.ts
var pathMenu = null;
var pathMenuAnchor = null;
var pathMenuCleanup = null;
var FILE_ACTION_TEXT = {
  open: "在飞牛中打开",
  details: "文件详情",
  quote: "引用到对话",
  copy: "复制路径"
};
function togglePathMenu(anchor, target) {
  if (pathMenu && pathMenuAnchor === anchor) {
    closePathMenu();
    return;
  }
  openPathMenu(anchor, target);
}
function openPathMenu(anchor, target) {
  closePathMenu();
  const { path, entry } = target;
  const actions = fileRowActions(entry, isFnosHost());
  if (!path || !actions.length) return;
  const menu = document.createElement("div");
  menu.className = "dsh-fnos-file-menu";
  menu.setAttribute("role", "menu");
  const title = document.createElement("div");
  title.className = "dsh-fnos-file-menu-title";
  title.textContent = path.split("/").filter(Boolean).pop() || path;
  title.title = path;
  menu.append(title);
  for (const action of actions) {
    const item = document.createElement("button");
    item.type = "button";
    item.className = "dsh-fnos-file-menu-item";
    item.setAttribute("role", "menuitem");
    const text = document.createElement("span");
    text.className = "dsh-fnos-file-menu-label";
    text.textContent = t(FILE_ACTION_TEXT[action]);
    item.append(text);
    item.addEventListener("click", (event) => {
      event.preventDefault();
      event.stopPropagation();
      closePathMenu();
      void runPathAction(action, path, entry);
    });
    menu.appendChild(item);
  }
  document.body.appendChild(menu);
  positionMenu(menu, anchor);
  pathMenu = menu;
  pathMenuAnchor = anchor;
  const onPointerDown = (event) => {
    if (!(event.target instanceof Node)) return;
    if (menu.contains(event.target) || anchor.contains(event.target)) return;
    closePathMenu();
  };
  const onKeyDown = (event) => {
    if (event.key === "Escape") closePathMenu();
  };
  document.addEventListener("pointerdown", onPointerDown, true);
  document.addEventListener("keydown", onKeyDown, true);
  window.addEventListener("scroll", closePathMenu, true);
  window.addEventListener("resize", closePathMenu);
  pathMenuCleanup = () => {
    document.removeEventListener("pointerdown", onPointerDown, true);
    document.removeEventListener("keydown", onKeyDown, true);
    window.removeEventListener("scroll", closePathMenu, true);
    window.removeEventListener("resize", closePathMenu);
  };
}
function positionMenu(menu, anchor) {
  const margin = 8;
  const rect = anchor.getBoundingClientRect();
  const width = menu.offsetWidth || 200;
  const height = menu.offsetHeight || 100;
  menu.style.left = `${Math.min(Math.max(margin, rect.right - width), window.innerWidth - width - margin)}px`;
  let top = rect.bottom + 6;
  if (top + height > window.innerHeight - margin) top = Math.max(margin, rect.top - height - 6);
  menu.style.top = `${top}px`;
}
function closePathMenu() {
  if (pathMenuCleanup) {
    try {
      pathMenuCleanup();
    } catch {
    }
    pathMenuCleanup = null;
  }
  if (pathMenu) {
    pathMenu.remove();
    pathMenu = null;
  }
  pathMenuAnchor = null;
}
async function runPathAction(action, path, entry) {
  const isDirectory = entry === "directory";
  try {
    if (action === "open") {
      await openPathInFnos(path, isDirectory);
    } else if (action === "details") {
      await showPathDetails(path);
    } else if (action === "quote") {
      await quotePathToComposer(path);
    } else if (action === "copy") {
      await copyPathToClipboard(path);
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    console.warn(LOG_PREFIX, "path action failed", action, message, error);
    if (action === "open") {
      showToast(t("在飞牛中打开失败，当前宿主可能不支持该操作。"));
    } else if (action === "details") {
      showToast(t("无法显示文件详情，当前宿主可能不支持该操作。"));
    } else {
      showToast(translateError(message));
    }
  }
}
async function openPathInFnos(path, isDirectory) {
  const sdk = getSdk();
  try {
    await sdk.ready();
  } catch {
  }
  if (isDirectory || typeof sdk.openFile !== "function") {
    await sdk.openFileManager(path);
    return;
  }
  try {
    const opened = await sdk.openFile(path);
    if (opened === false) throw new Error("openFile is not supported by this host");
  } catch (error) {
    console.warn(LOG_PREFIX, "openFile failed; falling back to the file manager", error);
    await sdk.openFileManager(path);
  }
}
async function showPathDetails(path) {
  const sdk = getSdk();
  try {
    await sdk.ready();
  } catch {
  }
  if (typeof sdk.showFileDetails !== "function") {
    throw new Error("showFileDetails is not supported by this host");
  }
  const result = await sdk.showFileDetails([path]);
  if (result === null || result === false) {
    throw new Error("showFileDetails is not supported by this host");
  }
}
async function copyPathToClipboard(path) {
  const ok = await copyText(path);
  showToast(ok ? t("已复制路径") : t("复制失败，请手动复制"));
}

// src/client/file-tree.ts
function scanFileTree() {
  for (const row of document.querySelectorAll("[data-files-entry][data-files-path]")) {
    const entry = row.getAttribute("data-files-entry") || "";
    const action = row.querySelector(`:scope > [${FILE_ACTION_ATTR}]`);
    if (entry !== "file" && entry !== "directory") {
      if (action) action.remove();
      row.classList.remove("dsh-fnos-file-row");
      if (row.dataset.dshFnosPosition === "1") {
        row.style.position = "";
        delete row.dataset.dshFnosPosition;
      }
      continue;
    }
    if (action) {
      const rowButton = row.querySelector(":scope > button");
      const height = rowButton ? rowButton.offsetHeight : 0;
      if (height && action.style.height !== `${height}px`) action.style.height = `${height}px`;
      const label = t("飞牛操作");
      if (action.title !== label) action.title = label;
      if (action.getAttribute("aria-label") !== label) action.setAttribute("aria-label", label);
      continue;
    }
    injectFileAction(row);
  }
}
function injectFileAction(row) {
  const rowButton = row.querySelector(":scope > button");
  if (!rowButton) return;
  row.classList.add("dsh-fnos-file-row");
  if (getComputedStyle(row).position === "static") {
    row.style.position = "relative";
    row.dataset.dshFnosPosition = "1";
  }
  const action = document.createElement("button");
  action.type = "button";
  action.setAttribute(FILE_ACTION_ATTR, "1");
  action.className = "dsh-fnos-file-action";
  action.innerHTML = FILE_ACTION_SVG;
  action.title = t("飞牛操作");
  action.setAttribute("aria-label", t("飞牛操作"));
  action.style.height = `${rowButton.offsetHeight || 28}px`;
  action.addEventListener("mousedown", (event) => {
    event.preventDefault();
    event.stopPropagation();
  });
  action.addEventListener("click", (event) => {
    event.preventDefault();
    event.stopPropagation();
    togglePathMenu(action, {
      path: row.getAttribute("data-files-path") || "",
      entry: row.getAttribute("data-files-entry") || ""
    });
  });
  row.appendChild(action);
}

// src/client/reference-menu.ts
var REFERENCE_OPTION_ID_RE = /^dsh-slash-option-reference-/;
function scanReferenceMenu() {
  for (const menu of document.querySelectorAll("[data-trigger-menu]")) {
    if (!isVisible(menu)) continue;
    if (menu.querySelector(`[${REF_PICK_ITEM_ATTR}]`)) continue;
    const options = Array.from(menu.querySelectorAll('button[role="option"]'));
    const first = options.find((option) => REFERENCE_OPTION_ID_RE.test(option.id));
    if (!first) continue;
    injectPickItem(first);
  }
}
function injectPickItem(template) {
  const item = template.cloneNode(true);
  item.removeAttribute("id");
  item.removeAttribute("aria-selected");
  item.removeAttribute("aria-label");
  item.setAttribute(REF_PICK_ITEM_ATTR, "1");
  if (item.className) {
    item.className = item.className.split(/\s+/).filter((name2) => name2 && !/active/i.test(name2)).join(" ");
  }
  const spans = Array.from(item.querySelectorAll("span"));
  const icon = spans.find((span) => span.querySelector("svg")) || spans[0];
  const name = icon ? icon.nextElementSibling : spans[1];
  const detail = name ? name.nextElementSibling : null;
  if (icon) icon.innerHTML = PICK_FILE_SVG;
  if (name) name.textContent = t("从飞牛选择");
  if (detail) detail.textContent = t("NAS 文件");
  item.addEventListener("mousedown", (event) => {
    event.preventDefault();
    event.stopPropagation();
  });
  item.addEventListener("click", (event) => {
    event.preventDefault();
    event.stopPropagation();
    dismissTriggerMenu();
    void handlePickFiles(item);
  });
  template.before(item);
}

// src/client/settings-page.tsx
var React = __toESM(require("react"), 1);

// src/version.ts
var VERSION = "0.6.3";

// src/client/workspace-menu.ts
var lastWorkspaceMenu = null;
var MENU_TTL_MS = 5 * 60 * 1e3;
function noteWorkspaceRowClick(row) {
  const key = row.getAttribute("data-row-key") || "";
  const id = key.startsWith("workspace:") ? key.slice("workspace:".length) : "";
  if (id) lastWorkspaceMenu = { id, at: Date.now() };
}
function scanWorkspaceMenus() {
  if (!lastWorkspaceMenu || Date.now() - lastWorkspaceMenu.at > MENU_TTL_MS) return;
  for (const menu of document.querySelectorAll('[role="menu"]')) {
    if (!isVisible(menu)) continue;
    if (menu.querySelector(`[${CHECK_DIR_ATTR}]`)) continue;
    if (!isWorkspaceActionsMenu(menu)) continue;
    injectOpenDirItem(menu);
  }
}
function isWorkspaceActionsMenu(menu) {
  const text = (menu.textContent || "").replace(/\s+/g, " ");
  return /Delete workspace|删除工作区|删除工作空间|删除工作區/i.test(text);
}
function injectOpenDirItem(menu) {
  const viewport = menu.querySelector('[role="presentation"]') || menu;
  const templateButton = menu.querySelector('button[role="menuitem"]');
  const template = templateButton && templateButton.closest("div");
  if (!template) return;
  const make = (attr, label, icon, onClick) => {
    const item = template.cloneNode(true);
    item.setAttribute(attr, "1");
    const button = item.querySelector('button[role="menuitem"]');
    if (!button) return null;
    const spans = Array.from(button.querySelectorAll("span"));
    const labelNode = spans.find((span) => /Rename|重命名|Delete|删除/i.test(span.textContent || "")) || spans[spans.length - 1];
    if (labelNode) labelNode.textContent = label;
    const iconNode = spans.find((span) => span !== labelNode);
    if (iconNode) iconNode.innerHTML = icon;
    button.addEventListener("click", (event) => {
      event.preventDefault();
      event.stopPropagation();
      const context = lastWorkspaceMenu;
      closeMenu(menu);
      if (!context) {
        showToast(t("无法确定工作区"));
        return;
      }
      onClick(context.id);
    });
    return item;
  };
  const open = isFnosHost() ? make(OPEN_DIR_ATTR, t("打开目录"), FOLDER_OPEN_SVG, (id) => {
    void openWorkspaceDirectory(id);
  }) : null;
  const check = make(CHECK_DIR_ATTR, t("检查目录权限"), FOLDER_OPEN_SVG, (id) => {
    void checkWorkspaceFromMenu(id);
  });
  const reauth = make(REAUTH_DIR_ATTR, t("重新授权目录"), FOLDER_OPEN_SVG, (id) => {
    let popup = null;
    if (!isFnosHost() && !isFnosAppEmbedded()) popup = openAuthPopup();
    void reauthWorkspaceFromMenu(id, popup);
  });
  if (reauth) viewport.insertBefore(reauth, viewport.firstChild);
  if (check) viewport.insertBefore(check, viewport.firstChild);
  if (open) viewport.insertBefore(open, viewport.firstChild);
}
function closeMenu(menu) {
  try {
    const options = {
      key: "Escape",
      code: "Escape",
      keyCode: 27,
      which: 27,
      bubbles: true
    };
    document.dispatchEvent(new KeyboardEvent("keydown", options));
    window.dispatchEvent(new KeyboardEvent("keydown", options));
  } catch {
  }
  window.setTimeout(() => {
    if (menu.isConnected) menu.remove();
  }, 0);
}
async function openWorkspaceDirectory(id) {
  const path = await resolveWorkspacePath(id);
  if (!path) {
    showToast(t("无法获取工作区目录"));
    return;
  }
  const sdk = getSdk();
  try {
    await sdk.ready();
  } catch {
  }
  try {
    await sdk.openFileManager(path);
  } catch (error) {
    console.warn(LOG_PREFIX, "openFileManager failed", error);
    showToast(t("打开目录失败，当前宿主可能不支持该操作。"));
  }
}
async function checkWorkspaceFromMenu(id) {
  showToast(t("正在检查目录权限…"));
  try {
    const [result] = await checkWorkspaceHealth([id]);
    showToast(workspaceHealthText(result));
  } catch (error) {
    showToast(t("检查失败：{error}", {
      error: translateError(error instanceof Error ? error.message : error)
    }));
  }
}
async function reauthWorkspaceFromMenu(id, popup) {
  const path = await resolveWorkspacePath(id);
  if (!path) {
    closePopup(popup);
    showToast(t("无法获取工作区目录"));
    return;
  }
  try {
    await authorizeWorkspacePath(path, popup);
    const [result] = await checkWorkspaceHealth([id]);
    showToast(`${t("已重新授权")} · ${workspaceHealthText(result)}`);
  } catch (error) {
    closePopup(popup);
    showToast(t("重新授权失败：{error}", {
      error: translateError(error instanceof Error ? error.message : error)
    }));
  }
}
async function authorizeWorkspacePath(path, popup) {
  if (isFnosHost()) {
    const sdk = getSdk();
    try {
      await sdk.ready();
    } catch {
    }
    const result = await sdk.authorizeUserFile(path);
    if (result && result.code === 0) return true;
    throw new Error(result && result.msg ? translateError(result.msg) : t("重新授权失败：{error}", { error: "EACCES" }));
  }
  const config = await loadConfig();
  const settings = await loadSettings();
  const target = await resolveAuthTargets(config, settings);
  let base;
  if (target.mode === "choose") {
    const preferred = target.choices.find((choice) => choice.id === settings.authMode) || target.choices[0];
    base = preferred ? preferred.base : resolveAuthBase();
  } else {
    base = target.base;
  }
  let frame = null;
  let windowRef = popup;
  let surface;
  if (isFnosAppEmbedded()) {
    frame = openAuthFrame();
    if (!frame) throw new Error(t("无法打开授权页面，请重试。"));
    surface = frame.surface;
  } else {
    if (!windowRef || windowRef.closed) windowRef = openAuthPopup();
    if (!windowRef) throw new Error(t("浏览器拦截了授权窗口，请允许弹窗后重试。"));
    surface = windowSurface(windowRef);
  }
  try {
    const state = randomId();
    const auth = await requestAuth({
      surface,
      state,
      buildUrl: () => buildAuthUrl(base, state, "authorizeUserFile", { path }, config.appName || "Harness")
    });
    if (auth && auth.status === "success") return true;
    if (isAuthCancelled(auth)) {
      throw new Error(t("已取消授权"));
    }
    throw new Error(auth && auth.error ? translateError(auth.error) : t("打开选择器失败"));
  } catch (error) {
    closePopup(windowRef);
    throw error;
  } finally {
    frame?.close();
  }
}

// src/client/settings-page.tsx
var hintStyle = {
  margin: 0,
  fontSize: 12,
  lineHeight: "18px",
  color: "var(--dsw-alias-label-tertiary, #888)"
};
var errorHintStyle = {
  ...hintStyle,
  color: "var(--dsw-alias-state-error-primary, #d93025)"
};
var inputStyle = {
  boxSizing: "border-box",
  width: "100%",
  height: 36,
  padding: "0 10px",
  borderRadius: 8,
  border: "1px solid var(--dsw-alias-border-l2, rgba(127,127,127,.32))",
  background: "transparent",
  color: "var(--dsw-alias-label-primary, inherit)",
  font: "inherit",
  fontSize: 14,
  outline: "none"
};
var labelStyle = {
  display: "flex",
  flexDirection: "column",
  gap: 6,
  fontSize: 13,
  color: "var(--dsw-alias-label-secondary, #666)"
};
var smallButtonStyle = {
  boxSizing: "border-box",
  height: 32,
  padding: "0 12px",
  borderRadius: 8,
  border: "1px solid var(--dsw-alias-border-l2, rgba(127,127,127,.35))",
  background: "transparent",
  color: "var(--dsw-alias-label-primary, inherit)",
  font: "inherit",
  fontSize: 13,
  cursor: "pointer"
};
function SettingsSlot(props) {
  if (props && props.view === "summary") return settingsSummary();
  return /* @__PURE__ */ React.createElement(SettingsPage, null);
}
function settingsSummary() {
  const settings = getCachedSettings();
  const proxy = settings.proxyBase || t("未设置");
  return t("反代 {proxy} · 内网 {lan}", { proxy, lan: settings.lanTarget });
}
function CollapsibleSection({ title, description, defaultOpen, children }) {
  const [open, setOpen] = React.useState(defaultOpen !== false);
  return /* @__PURE__ */ React.createElement("section", { style: {
    display: "flex",
    flexDirection: "column",
    gap: 8,
    paddingTop: 10,
    borderTop: "1px solid var(--dsw-alias-border-l3, rgba(127,127,127,.22))"
  } }, /* @__PURE__ */ React.createElement(
    "button",
    {
      type: "button",
      "aria-expanded": open,
      "aria-label": `${title} · ${open ? t("收起") : t("展开")}`,
      onClick: () => setOpen((value) => !value),
      style: {
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 12,
        width: "100%",
        padding: "2px 0",
        border: "none",
        background: "transparent",
        color: "inherit",
        font: "inherit",
        textAlign: "left",
        cursor: "pointer"
      }
    },
    /* @__PURE__ */ React.createElement("span", { style: { fontSize: 14, fontWeight: 600 } }, title),
    /* @__PURE__ */ React.createElement("span", { "aria-hidden": true, style: {
      color: "var(--dsw-alias-label-tertiary, #888)",
      fontSize: 14,
      transform: open ? "rotate(90deg)" : "none",
      transition: "transform .12s ease"
    } }, "›")
  ), description ? /* @__PURE__ */ React.createElement("p", { style: { ...hintStyle, display: open ? "block" : "none" } }, description) : null, /* @__PURE__ */ React.createElement("div", { style: { display: open ? "block" : "none" } }, children));
}
function SettingsPage() {
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);
  const [settings, setSettings] = React.useState(() => ({ ...DEFAULT_SETTINGS }));
  const [message, setMessage] = React.useState("");
  const [error, setError] = React.useState("");
  const [embedded, setEmbedded] = React.useState(false);
  const [gatewayAccess, setGatewayAccess] = React.useState(/^\/app(?:\/|$)/.test(window.location.pathname));
  const [hostState2, setHostState] = React.useState("unknown");
  const [hostVersion, setHostVersion] = React.useState("");
  const [language, setLanguage] = React.useState(getLanguagePreference());
  const [, setLanguageVersion] = React.useState(0);
  React.useEffect(() => {
    const unsubscribe = onLanguageChange(() => setLanguageVersion((value) => value + 1));
    if (getLanguagePreference() === "system") void resolveHostLanguage();
    return unsubscribe;
  }, []);
  React.useEffect(() => {
    let alive = true;
    loadSettings(true).then((loaded2) => {
      if (!alive) return;
      setSettings(loaded2);
      setHostState(getSettingsHostState());
      setLoading(false);
    }).catch(() => {
      if (alive) setLoading(false);
    });
    loadConfig().then((config) => {
      if (alive) setHostVersion(config.version || "");
    }).catch(() => {
    });
    const sdk = getSdk();
    sdk.ready().then(() => {
      if (alive) setEmbedded(isFnosHost());
    }).catch(() => {
    });
    if (!/^\/app(?:\/|$)/.test(window.location.pathname)) {
      loadConfig().then((config) => authRouteExists(config.appName || "Harness")).then((ok) => {
        if (alive) setGatewayAccess(ok);
      }).catch(() => {
      });
    }
    return () => {
      alive = false;
    };
  }, []);
  const update = (key) => (event) => {
    const value = event.target.value;
    setSettings((current) => ({
      ...current,
      [key]: key === "authMode" ? value : value
    }));
    setMessage("");
    setError("");
  };
  const save = async () => {
    setSaving(true);
    setMessage("");
    setError("");
    try {
      const saved = await saveSettings(settings);
      setSettings(normalizeClientSettings(saved));
      setMessage(t("已保存"));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };
  const hostOutdated = Boolean(hostVersion && hostVersion !== VERSION);
  const hostReady = hostState2 === "ok" && !hostOutdated;
  const hostWarning = hostReady ? "" : hostOutdated ? t("宿主半侧版本 {host}，客户端 {client}。请重启 dsh 后再保存设置。", { host: hostVersion, client: VERSION }) : hostState2 === "missing" ? t("宿主半侧未提供设置接口，通常是插件升级后没有重启 dsh。请重启 dsh 后再保存设置。") : hostState2 === "error" ? t("无法读取宿主设置接口，请确认 dsh 运行正常。") : "";
  const disabled = loading || saving || !hostReady;
  const buttonStyle = {
    boxSizing: "border-box",
    height: 36,
    padding: "0 16px",
    borderRadius: 8,
    border: "1px solid var(--dsw-alias-border-l2, rgba(127,127,127,.35))",
    background: "transparent",
    color: "var(--dsw-alias-label-primary, inherit)",
    font: "inherit",
    fontSize: 14,
    fontWeight: 500,
    cursor: disabled ? "default" : "pointer",
    opacity: disabled ? 0.6 : 1
  };
  const accessNote = isFnosApp() ? t("当前运行在飞牛 App 内：目录授权打开平台的网页授权页，文件详情/在飞牛中打开仍走宿主桥接。") : embedded ? t("当前运行在飞牛桌面/宿主内：会直接调用飞牛目录选择器，不打开网页授权。") : gatewayAccess ? t("当前通过飞牛网关访问：直接使用当前同源地址授权，不会弹出入口选择。") : t("当前不是通过飞牛网关访问：同时配置反代与内网地址后，点击「选择飞牛目录」会让你选择授权入口。");
  return /* @__PURE__ */ React.createElement("div", { style: {
    display: "flex",
    flexDirection: "column",
    gap: 14,
    padding: "4px 0 10px",
    maxWidth: 640,
    color: "var(--dsw-alias-label-primary, inherit)",
    fontSize: 14
  } }, /* @__PURE__ */ React.createElement("div", { style: { display: "flex", flexDirection: "column", gap: 4 } }, /* @__PURE__ */ React.createElement("div", { style: { fontSize: 15, fontWeight: 600 } }, t("飞牛目录授权")), /* @__PURE__ */ React.createElement("p", { style: hintStyle }, t("配置非网关访问时使用的飞牛授权入口；设置保存在当前 dsh 数据目录。"))), /* @__PURE__ */ React.createElement(
    CollapsibleSection,
    {
      title: t("连接与授权"),
      description: t("配置反代/内网授权入口、界面语言和图片读取。"),
      defaultOpen: true
    },
    /* @__PURE__ */ React.createElement("label", { style: labelStyle }, /* @__PURE__ */ React.createElement("span", null, t("反代飞牛地址")), /* @__PURE__ */ React.createElement(
      "input",
      {
        type: "text",
        value: settings.proxyBase,
        onChange: update("proxyBase"),
        placeholder: "https://fnos.example.com",
        autoComplete: "off",
        spellCheck: false,
        disabled,
        style: inputStyle
      }
    ), /* @__PURE__ */ React.createElement("span", { style: hintStyle }, t("填协议 + 域名[:端口]，不要带路径；留空表示未配置。"))),
    /* @__PURE__ */ React.createElement("label", { style: labelStyle }, /* @__PURE__ */ React.createElement("span", null, t("飞牛内网地址 / 端口")), /* @__PURE__ */ React.createElement(
      "input",
      {
        type: "text",
        value: settings.lanTarget,
        onChange: update("lanTarget"),
        placeholder: "5666 或 http://192.168.1.10:5666",
        autoComplete: "off",
        spellCheck: false,
        disabled,
        style: inputStyle
      }
    ), /* @__PURE__ */ React.createElement("span", { style: hintStyle }, t("只填端口时使用当前访问主机的地址；默认 5666。"))),
    /* @__PURE__ */ React.createElement("label", { style: labelStyle }, /* @__PURE__ */ React.createElement("span", null, t("非网关访问时的授权入口")), /* @__PURE__ */ React.createElement("select", { value: settings.authMode, onChange: update("authMode"), disabled, style: inputStyle }, /* @__PURE__ */ React.createElement("option", { value: "ask" }, t("每次询问（推荐）")), /* @__PURE__ */ React.createElement("option", { value: "proxy" }, t("优先使用反代地址")), /* @__PURE__ */ React.createElement("option", { value: "lan" }, t("优先使用内网地址")))),
    /* @__PURE__ */ React.createElement("label", { style: labelStyle }, /* @__PURE__ */ React.createElement("span", null, t("界面语言")), /* @__PURE__ */ React.createElement(
      "select",
      {
        value: language,
        onChange: (event) => {
          const next = setLanguagePreference(event.target.value);
          setLanguage(next);
          if (next === "system") void resolveHostLanguage(true);
          setMessage("");
          setError("");
        },
        disabled,
        style: inputStyle
      },
      /* @__PURE__ */ React.createElement("option", { value: "system" }, t("跟随系统")),
      /* @__PURE__ */ React.createElement("option", { value: "zh" }, t("中文")),
      /* @__PURE__ */ React.createElement("option", { value: "en" }, "English")
    )),
    /* @__PURE__ */ React.createElement("label", { style: { ...labelStyle, flexDirection: "row", alignItems: "flex-start", gap: 8 } }, /* @__PURE__ */ React.createElement(
      "input",
      {
        type: "checkbox",
        checked: settings.imageRead !== false,
        onChange: (event) => {
          const checked = event.target.checked;
          setSettings((current) => ({ ...current, imageRead: checked }));
          setMessage("");
          setError("");
        },
        disabled,
        style: { marginTop: 3 }
      }
    ), /* @__PURE__ */ React.createElement("span", { style: { display: "flex", flexDirection: "column", gap: 4 } }, /* @__PURE__ */ React.createElement("span", null, t("从飞牛选择图片时读取图片内容")), /* @__PURE__ */ React.createElement("span", { style: hintStyle }, t("图片需要字节才能被模型「看到」；关闭后图片也只生成路径引用。仅支持 PNG/JPEG/WebP/GIF，宿主只读取你选中的文件。")))),
    /* @__PURE__ */ React.createElement("p", { style: hintStyle }, accessNote),
    hostWarning ? /* @__PURE__ */ React.createElement("p", { style: errorHintStyle }, hostWarning) : null,
    /* @__PURE__ */ React.createElement("div", { style: { display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" } }, /* @__PURE__ */ React.createElement("button", { type: "button", onClick: save, disabled, style: buttonStyle }, saving ? t("保存中…") : t("保存")), loading ? /* @__PURE__ */ React.createElement("span", { style: hintStyle }, t("读取设置…")) : null, message ? /* @__PURE__ */ React.createElement("span", { style: { fontSize: 12, color: "var(--dsw-alias-state-success-primary, #188038)" } }, message) : null, error ? /* @__PURE__ */ React.createElement("span", { style: { fontSize: 12, color: "var(--dsw-alias-state-error-primary, #d93025)" } }, error) : null)
  ), /* @__PURE__ */ React.createElement(
    CollapsibleSection,
    {
      title: t("工作区目录权限"),
      description: t("检查 dsh 工作区目录的读写权限。飞牛 ACL 记录损坏或目录移动后，dsh 会读不到这些目录。"),
      defaultOpen: true
    },
    /* @__PURE__ */ React.createElement(WorkspaceHealthSection, { disabled })
  ), /* @__PURE__ */ React.createElement(
    CollapsibleSection,
    {
      title: t("诊断"),
      description: t("检查 dsh 版本、DOM 锚点和飞牛地址连通性。"),
      defaultOpen: false
    },
    /* @__PURE__ */ React.createElement(
      DiagnosticsSection,
      {
        settings,
        hostVersion,
        hostState: hostState2,
        embedded,
        gatewayAccess
      }
    )
  ));
}
function DiagnosticsSection({ settings, hostVersion, hostState: hostState2, embedded, gatewayAccess }) {
  const [proxyStatus, setProxyStatus] = React.useState(null);
  const [lanStatus, setLanStatus] = React.useState(null);
  const [copyMessage, setCopyMessage] = React.useState(null);
  const [health, setHealth] = React.useState(null);
  const [tick, setTick] = React.useState(0);
  const [appName, setAppName] = React.useState("Harness");
  const loadHealth = React.useCallback(() => {
    fetchPluginHealth().then((data) => setHealth(data)).catch(() => setHealth(null));
  }, []);
  React.useEffect(() => {
    let alive = true;
    loadConfig().then((config) => {
      if (alive && config.appName) setAppName(config.appName);
    }).catch(() => {
    });
    loadHealth();
    return () => {
      alive = false;
    };
  }, [loadHealth]);
  const statusStyle = (status) => ({
    fontSize: 12,
    color: status && status.ok === true ? "var(--dsw-alias-state-success-primary, #188038)" : status && status.ok === false ? "var(--dsw-alias-state-error-primary, #d93025)" : "var(--dsw-alias-label-tertiary, #888)"
  });
  const statusText = (status) => {
    if (!status) return "";
    if (status.reason === "notSet") return t("未设置");
    if (status.reason === "timeout") return t("连接超时");
    if (status.reason === "unreachable") return t("无法连接");
    return t("可连接（{ms}ms）", { ms: status.ms ?? 0 });
  };
  const composerAnchor = anchorObservation("composer");
  const fileTreeAnchor = anchorObservation("fileTree");
  const anchorClock = (at) => new Date(at).toLocaleTimeString(
    currentLanguage() === "en" ? "en-US" : "zh-CN",
    { hour: "2-digit", minute: "2-digit" }
  );
  const anchorText = (value) => {
    const kind = classifyAnchor(value);
    if (!value || kind === "never") return t("未检测");
    if (kind === "found") {
      const time = anchorClock(value.at);
      return value.count ? t("可用（{time} · {count} 行）", { time, count: value.count }) : t("可用（{time}）", { time });
    }
    if (kind === "empty") return t("未检测（文件树为空）");
    return t("不可用（{time}）", { time: anchorClock(value.at) });
  };
  const testAddress = async (kind) => {
    const setStatus = kind === "proxy" ? setProxyStatus : setLanStatus;
    const base = kind === "proxy" ? normalizeBase(settings.proxyBase || "") : lanBaseFromSettings(settings);
    if (!base) {
      setStatus({ reason: "notSet" });
      return;
    }
    setStatus({ pending: true });
    const controller = typeof AbortController === "function" ? new AbortController() : null;
    const timer = controller ? window.setTimeout(() => controller.abort(), 4e3) : 0;
    const started = Date.now();
    try {
      await fetch(
        `${base}/app-auth/pick-user-file?appName=${encodeURIComponent(appName)}&directory=true`,
        {
          mode: "no-cors",
          cache: "no-store",
          credentials: "omit",
          signal: controller ? controller.signal : void 0
        }
      );
      setStatus({ ok: true, ms: Date.now() - started });
    } catch (error) {
      setStatus({
        ok: false,
        reason: error instanceof Error && error.name === "AbortError" ? "timeout" : "unreachable"
      });
    } finally {
      if (timer) window.clearTimeout(timer);
    }
  };
  const runtime = isFnosApp() ? t("飞牛 App") : embedded ? t("飞牛桌面") : gatewayAccess ? t("飞牛网关") : t("普通浏览器");
  const hostStateText = hostState2 === "ok" ? t("正常") : hostState2 === "missing" ? t("缺失（需重启 dsh）") : hostState2 === "error" ? t("读取失败") : t("未检测");
  const imageReadText = health && health.features ? health.features.imageRead ? t("开") : t("关") : t("未检测");
  const workspaceHealthTextValue = health && health.features ? health.features.workspaceHealth ? t("可用") : t("不可用") : t("未检测");
  const copyDiagnostics = async () => {
    const lines = [
      `dsh-fnos-advanced ${VERSION}`,
      `${t("客户端")}: ${VERSION}`,
      `${t("宿主")}: ${hostVersion || "?"} (${hostStateText})`,
      `${t("运行环境")}: ${runtime}`,
      `${t("界面语言")}: ${currentLanguage()}`,
      `${t("对话输入框")}: ${anchorText(composerAnchor)}`,
      `${t("文件树")}: ${anchorText(fileTreeAnchor)}`,
      `${t("图片读取")}: ${imageReadText}`,
      `${t("工作区体检")}: ${workspaceHealthTextValue}`,
      `${t("反代飞牛地址")}: ${settings.proxyBase || t("未设置")}`,
      `${t("飞牛内网地址 / 端口")}: ${settings.lanTarget}`,
      `${t("测试反代地址")}: ${statusText(proxyStatus) || t("未检测")}`,
      `${t("测试内网地址")}: ${statusText(lanStatus) || t("未检测")}`
    ];
    const ok = await copyText(lines.join("\n"));
    setCopyMessage(Boolean(ok));
  };
  return /* @__PURE__ */ React.createElement("div", { key: `diagnostics-${tick}`, style: { display: "flex", flexDirection: "column", gap: 10, paddingTop: 4 } }, /* @__PURE__ */ React.createElement("div", { style: { display: "grid", gridTemplateColumns: "auto 1fr", gap: "4px 12px", fontSize: 12 } }, /* @__PURE__ */ React.createElement("span", { style: hintStyle }, t("宿主")), /* @__PURE__ */ React.createElement("span", null, `${hostVersion || "?"} · ${hostStateText}`), /* @__PURE__ */ React.createElement("span", { style: hintStyle }, t("客户端")), /* @__PURE__ */ React.createElement("span", null, VERSION), /* @__PURE__ */ React.createElement("span", { style: hintStyle }, t("运行环境")), /* @__PURE__ */ React.createElement("span", null, runtime), /* @__PURE__ */ React.createElement("span", { style: hintStyle }, t("图片读取")), /* @__PURE__ */ React.createElement("span", null, imageReadText), /* @__PURE__ */ React.createElement("span", { style: hintStyle }, t("对话输入框")), /* @__PURE__ */ React.createElement("span", null, anchorText(composerAnchor)), /* @__PURE__ */ React.createElement("span", { style: hintStyle }, t("文件树")), /* @__PURE__ */ React.createElement("span", null, anchorText(fileTreeAnchor)), /* @__PURE__ */ React.createElement("span", { style: hintStyle }, t("工作区体检")), /* @__PURE__ */ React.createElement("span", null, workspaceHealthTextValue)), /* @__PURE__ */ React.createElement("div", { style: hintStyle }, t("对话输入框与文件树只在对话页存在，这里显示最近一次在对应页面的检测结果。")), /* @__PURE__ */ React.createElement("div", { style: { display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" } }, /* @__PURE__ */ React.createElement("button", { type: "button", onClick: () => {
    void testAddress("proxy");
  }, style: smallButtonStyle }, t("测试反代地址")), proxyStatus ? /* @__PURE__ */ React.createElement("span", { style: statusStyle(proxyStatus) }, proxyStatus.pending ? t("测试中…") : statusText(proxyStatus)) : null, /* @__PURE__ */ React.createElement("button", { type: "button", onClick: () => {
    void testAddress("lan");
  }, style: smallButtonStyle }, t("测试内网地址")), lanStatus ? /* @__PURE__ */ React.createElement("span", { style: statusStyle(lanStatus) }, lanStatus.pending ? t("测试中…") : statusText(lanStatus)) : null), /* @__PURE__ */ React.createElement("div", { style: { display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" } }, /* @__PURE__ */ React.createElement("button", { type: "button", onClick: () => {
    void copyDiagnostics();
  }, style: smallButtonStyle }, t("复制诊断信息")), /* @__PURE__ */ React.createElement(
    "button",
    {
      type: "button",
      onClick: () => {
        setProxyStatus(null);
        setLanStatus(null);
        setCopyMessage(null);
        recordAnchorObservations();
        loadHealth();
        setTick((value) => value + 1);
      },
      style: smallButtonStyle
    },
    t("重新检测")
  ), copyMessage !== null ? /* @__PURE__ */ React.createElement("span", { style: hintStyle }, copyMessage ? t("诊断信息已复制") : t("复制失败")) : null));
}
function WorkspaceHealthSection({ disabled }) {
  const [results, setResults] = React.useState([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState("");
  const check = async () => {
    setLoading(true);
    setError("");
    try {
      setResults(await checkWorkspaceHealth([]));
    } catch (err) {
      setError(translateError(err instanceof Error ? err.message : err));
    } finally {
      setLoading(false);
    }
  };
  const reauth = async (result) => {
    let popup = null;
    if (!isFnosHost() && !isFnosAppEmbedded()) popup = openAuthPopup();
    try {
      await authorizeWorkspacePath(result.path, popup);
      const [next] = await checkWorkspaceHealth([result.id]);
      if (next) {
        setResults((current) => current.map((item) => item.id === next.id ? next : item));
      }
      showToast(t("已重新授权"));
    } catch (err) {
      closePopup(popup);
      showToast(t("重新授权失败：{error}", {
        error: translateError(err instanceof Error ? err.message : err)
      }));
    }
  };
  const buttonStyle = {
    ...smallButtonStyle,
    cursor: disabled || loading ? "default" : "pointer",
    opacity: disabled || loading ? 0.6 : 1
  };
  const rowStyle = {
    display: "flex",
    alignItems: "center",
    gap: 10,
    padding: "8px 0",
    borderTop: "1px solid var(--dsw-alias-border-l3, rgba(127,127,127,.2))"
  };
  return /* @__PURE__ */ React.createElement("div", { style: { display: "flex", flexDirection: "column", gap: 10, paddingTop: 4 } }, /* @__PURE__ */ React.createElement("div", { style: { display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" } }, /* @__PURE__ */ React.createElement(
    "button",
    {
      type: "button",
      onClick: () => {
        void check();
      },
      disabled: disabled || loading,
      style: buttonStyle
    },
    loading ? t("检查中…") : t("检查全部工作区")
  ), results.length ? /* @__PURE__ */ React.createElement("span", { style: hintStyle }, t("共 {count} 个", { count: results.length })) : null, error ? /* @__PURE__ */ React.createElement("span", { style: errorHintStyle }, error) : null, results.length || error ? /* @__PURE__ */ React.createElement(
    "button",
    {
      type: "button",
      onClick: () => {
        setResults([]);
        setError("");
      },
      disabled: loading,
      style: buttonStyle
    },
    t("清除结果")
  ) : null), results.length ? /* @__PURE__ */ React.createElement("div", null, results.map((result) => {
    const healthy = result.exists && result.readable && result.writable;
    return /* @__PURE__ */ React.createElement("div", { key: result.id, style: rowStyle }, /* @__PURE__ */ React.createElement("div", { style: { minWidth: 0, flex: 1, display: "flex", flexDirection: "column", gap: 2 } }, /* @__PURE__ */ React.createElement("span", { style: {
      fontSize: 13,
      fontWeight: 500,
      overflow: "hidden",
      textOverflow: "ellipsis",
      whiteSpace: "nowrap"
    } }, result.title || result.id), /* @__PURE__ */ React.createElement("span", { style: {
      ...hintStyle,
      overflow: "hidden",
      textOverflow: "ellipsis",
      whiteSpace: "nowrap"
    }, title: result.path }, result.path)), /* @__PURE__ */ React.createElement("span", { style: {
      flex: "none",
      fontSize: 12,
      color: healthy ? "var(--dsw-alias-state-success-primary, #188038)" : "var(--dsw-alias-state-error-primary, #d93025)"
    } }, workspaceHealthText(result)), /* @__PURE__ */ React.createElement(
      "button",
      {
        type: "button",
        onClick: () => {
          void reauth(result);
        },
        disabled,
        style: buttonStyle
      },
      t("重新授权")
    ));
  })) : null);
}

// src/client/settings-slot.ts
function registerSettingsSlot(ctx) {
  const inject = ctx?.inject;
  if (typeof inject !== "function") return;
  inject(["slots"], (scope) => {
    const slots = scope.slots;
    if (!slots || typeof slots.inject !== "function" || typeof slots.register !== "function") return;
    const install2 = () => {
      slots.inject("plugins.bundle.config", () => slots.register({ name: "plugins.bundle.config", key: SETTINGS_SLOT_KEY }, SettingsSlot));
    };
    if (typeof scope.effect === "function") {
      scope.effect(install2, "dsh-fnos-advanced: settings page");
    } else {
      install2();
    }
  });
}

// src/client/slot-actions.tsx
var React2 = __toESM(require("react"), 1);
var SLOT_ID = "dsh-fnos-advanced";
var SLOT_NAMES = [
  "deliverables.file.actions",
  "deliverables.review.file.actions",
  "sidebar.right.tab.document.actions",
  "sidebar.right.tab.document.unpreviewable"
];
var sessionCwd = () => "";
function sessionIdOf(props, root) {
  if (typeof props.sessionId === "string" && props.sessionId) return props.sessionId;
  return root?.closest("[data-sidebar-right-session]")?.getAttribute("data-sidebar-right-session") || "";
}
function resolveSlotPath(root, props) {
  if (typeof props.absolutePath === "string" && props.absolutePath.startsWith("/")) {
    return props.absolutePath;
  }
  const card = root?.closest("[data-presented-file]");
  const cardPath = card?.querySelector(":scope > button[title]")?.getAttribute("title") || "";
  if (cardPath) return resolveAbsolutePath(sessionCwd(sessionIdOf(props, root)), cardPath);
  const review = root?.closest("[data-changes-review]");
  if (review) {
    const relative = review.querySelector("[data-review-file]")?.getAttribute("data-review-file") || "";
    return resolveAbsolutePath(sessionCwd(sessionIdOf(props, review)), relative);
  }
  return "";
}
function FnosPathAction(props) {
  const anchorRef = React2.useRef(null);
  const label = t("飞牛操作");
  const [, forceLanguage] = React2.useReducer((value) => value + 1, 0);
  React2.useEffect(() => () => closePathMenu(), []);
  React2.useEffect(() => onLanguageChange(forceLanguage), [forceLanguage]);
  const onClick = (event) => {
    event.preventDefault();
    event.stopPropagation();
    const anchor = anchorRef.current;
    if (!anchor) return;
    const path = resolveSlotPath(anchor, props);
    if (!path) {
      showToast(t("无法确定文件路径"));
      return;
    }
    togglePathMenu(anchor, { path, entry: "file" });
  };
  return /* @__PURE__ */ React2.createElement(
    "button",
    {
      ref: anchorRef,
      type: "button",
      className: "dsh-fnos-slot-action",
      ...{ [SLOT_ACTION_ATTR]: "1" },
      title: label,
      "aria-label": label,
      onMouseDown: (event) => {
        event.stopPropagation();
      },
      onClick,
      dangerouslySetInnerHTML: { __html: FILE_ACTION_SVG }
    }
  );
}
function registerPathActionSlots(ctx) {
  const inject = ctx?.inject;
  if (typeof inject !== "function") return;
  inject(["slots"], (scope) => {
    const slots = scope.slots;
    if (!slots || typeof slots.inject !== "function" || typeof slots.register !== "function") return;
    const install2 = () => {
      for (const name of SLOT_NAMES) {
        slots.inject(name, () => slots.register({ name, id: SLOT_ID }, FnosPathAction));
      }
    };
    if (typeof scope.effect === "function") scope.effect(install2, "dsh-fnos-advanced: file actions");
    else install2();
  });
}
function registerSessionCwd(ctx) {
  const inject = ctx?.inject;
  if (typeof inject !== "function") return;
  inject(["sessions"], (scope) => {
    const list = scope.sessions?.list;
    if (!list || typeof list.getSnapshot !== "function") return;
    sessionCwd = (sessionId) => {
      if (!sessionId) return "";
      try {
        const cwd = list.getSnapshot?.()?.byId?.[sessionId]?.cwd;
        return typeof cwd === "string" ? cwd : "";
      } catch {
        return "";
      }
    };
  });
}

// src/client/style.ts
var STYLE_TEXT = `
  .dsh-fnos-advanced-button {
    box-sizing: border-box;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    height: 36px;
    padding: 0 14px;
    border: 1px solid var(--dsw-alias-border-l2, rgba(127, 127, 127, .35));
    border-radius: var(--dsw-radius-md, 8px);
    background: transparent;
    color: var(--dsw-alias-label-primary, inherit);
    font: inherit;
    font-size: 14px;
    font-weight: 500;
    line-height: 1;
    white-space: nowrap;
    cursor: pointer;
    transition: background-color .15s ease, border-color .15s ease;
  }
  .dsh-fnos-advanced-button:hover:not(:disabled) {
    background: var(--dsw-alias-interactive-bg-hover, rgba(127, 127, 127, .1));
    border-color: var(--dsw-alias-border-l1, rgba(127, 127, 127, .5));
  }
  .dsh-fnos-advanced-button:focus-visible {
    outline: var(--dsw-focus-ring-width, 2px) solid var(--dsw-alias-state-business-primary, #4f7cff);
    outline-offset: 1px;
  }
  .dsh-fnos-advanced-button:disabled {
    opacity: .62;
    cursor: default;
  }
  .dsh-fnos-recent {
    box-sizing: border-box;
    display: flex;
    flex: 1 0 100%;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-start;
    gap: 6px;
    min-width: 0;
    margin-bottom: 6px;
  }
  .dsh-fnos-recent-label {
    flex: none;
    color: var(--dsw-alias-label-tertiary, #888);
    font-size: 11px;
    line-height: 16px;
  }
  .dsh-fnos-recent-chip,
  .dsh-fnos-recent-clear {
    box-sizing: border-box;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    max-width: 160px;
    height: 26px;
    padding: 0 8px;
    border: 1px solid var(--dsw-alias-border-l2, rgba(127, 127, 127, .32));
    border-radius: 999px;
    background: transparent;
    color: var(--dsw-alias-label-secondary, inherit);
    font: inherit;
    font-size: 12px;
    line-height: 1;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    cursor: pointer;
  }
  .dsh-fnos-recent-chip:hover,
  .dsh-fnos-recent-clear:hover {
    background: var(--dsw-alias-interactive-bg-hover, rgba(127, 127, 127, .1));
    color: var(--dsw-alias-label-primary, inherit);
  }
  .dsh-fnos-recent-chip:focus-visible,
  .dsh-fnos-recent-clear:focus-visible {
    outline: var(--dsw-focus-ring-width, 2px) solid var(--dsw-alias-state-business-primary, #4f7cff);
    outline-offset: 1px;
  }
  .dsh-fnos-recent-clear {
    max-width: none;
    width: 26px;
    padding: 0;
    color: var(--dsw-alias-label-tertiary, #888);
  }
  .dsh-fnos-file-row > button {
    padding-right: 34px;
  }
  .dsh-fnos-file-action {
    position: absolute;
    top: 0;
    right: 6px;
    box-sizing: border-box;
    display: grid;
    place-items: center;
    width: 24px;
    height: 28px;
    padding: 0;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--dsw-alias-label-tertiary, #888);
    cursor: pointer;
    opacity: 0;
    pointer-events: none;
    transition: opacity .12s ease, background-color .12s ease;
  }
  .dsh-fnos-file-row:hover > .dsh-fnos-file-action,
  .dsh-fnos-file-row:focus-within > .dsh-fnos-file-action,
  .dsh-fnos-file-row > .dsh-fnos-file-action:focus-visible {
    opacity: 1;
    pointer-events: auto;
  }
  .dsh-fnos-file-action:hover {
    background: transparent;
    color: var(--dsw-alias-label-primary, inherit);
  }
  .dsh-fnos-file-action:focus-visible {
    outline: var(--dsw-focus-ring-width, 2px) solid var(--dsw-alias-state-business-primary, #4f7cff);
    outline-offset: 1px;
  }
  @media (hover: none) {
    .dsh-fnos-file-row > .dsh-fnos-file-action {
      opacity: 1;
      pointer-events: auto;
    }
  }
  .dsh-fnos-slot-action {
    box-sizing: border-box;
    display: inline-flex;
    flex: none;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    padding: 0;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--dsw-alias-label-tertiary, #888);
    cursor: pointer;
  }
  .dsh-fnos-slot-action:hover {
    background: var(--dsw-alias-interactive-bg-hover, rgba(127, 127, 127, .1));
    color: var(--dsw-alias-label-primary, inherit);
  }
  .dsh-fnos-slot-action:focus-visible {
    outline: var(--dsw-focus-ring-width, 2px) solid var(--dsw-alias-state-business-primary, #4f7cff);
    outline-offset: 1px;
  }
  .dsh-fnos-file-menu {
    position: fixed;
    z-index: 10000;
    box-sizing: border-box;
    width: 200px;
    padding: 6px;
    border: 1px solid var(--dsw-alias-border-l2, rgba(127, 127, 127, .32));
    border-radius: 10px;
    background: var(--dsw-alias-bg-layer-1, #fff);
    color: var(--dsw-alias-label-primary, inherit);
    box-shadow: 0 12px 40px rgba(0, 0, 0, .18);
  }
  .dsh-fnos-file-menu-title {
    margin-bottom: 4px;
    padding: 2px 8px 6px;
    font-size: 13px;
    font-weight: 600;
    line-height: 18px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dsh-fnos-file-menu-item {
    box-sizing: border-box;
    display: flex;
    align-items: center;
    width: 100%;
    padding: 7px 8px;
    border: none;
    border-radius: 7px;
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .dsh-fnos-file-menu-item:hover {
    background: var(--dsw-alias-interactive-bg-hover, rgba(127, 127, 127, .1));
  }
  .dsh-fnos-file-menu-label {
    font-size: 13px;
    font-weight: 500;
    line-height: 18px;
  }
  .dsh-fnos-advanced-status {
    position: absolute;
    left: 24px;
    right: 24px;
    bottom: 70px;
    z-index: 10;
    display: none;
    box-sizing: border-box;
    padding: 8px 12px;
    border-radius: 8px;
    background: var(--dsw-alias-bg-layer-2, rgba(127, 127, 127, .12));
    color: var(--dsw-alias-label-secondary, inherit);
    font-size: 12px;
    line-height: 18px;
    overflow-wrap: anywhere;
    pointer-events: none;
  }
  .dsh-fnos-advanced-status[data-error="1"] {
    color: var(--dsw-alias-state-error-primary, #d93025);
  }
  .dsh-fnos-advanced-chooser {
    position: absolute;
    left: 24px;
    right: 24px;
    bottom: 70px;
    z-index: 30;
    display: flex;
    flex-direction: column;
    gap: 8px;
    box-sizing: border-box;
    padding: 12px;
    border: 1px solid var(--dsw-alias-border-l2, rgba(127, 127, 127, .32));
    border-radius: 12px;
    background: var(--dsw-alias-bg-layer-1, #fff);
    color: var(--dsw-alias-label-primary, inherit);
    box-shadow: 0 12px 40px rgba(0, 0, 0, .18);
  }
  .dsh-fnos-advanced-chooser-title {
    font-size: 13px;
    font-weight: 600;
    line-height: 18px;
  }
  .dsh-fnos-advanced-chooser-option,
  .dsh-fnos-advanced-chooser-cancel {
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
    width: 100%;
    padding: 8px 10px;
    border: 1px solid var(--dsw-alias-border-l2, rgba(127, 127, 127, .32));
    border-radius: 8px;
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .dsh-fnos-advanced-chooser-option:hover,
  .dsh-fnos-advanced-chooser-cancel:hover {
    background: var(--dsw-alias-interactive-bg-hover, rgba(127, 127, 127, .1));
  }
  .dsh-fnos-advanced-chooser-label {
    font-size: 13px;
    font-weight: 500;
    line-height: 18px;
  }
  .dsh-fnos-advanced-chooser-detail {
    max-width: 100%;
    overflow-wrap: anywhere;
    color: var(--dsw-alias-label-tertiary, #888);
    font-size: 11px;
    line-height: 16px;
  }
  .dsh-fnos-advanced-chooser-cancel {
    align-items: center;
    color: var(--dsw-alias-label-secondary, #666);
    font-size: 12px;
  }
  .dsh-fnos-advanced-toast {
    position: fixed;
    left: 50%;
    bottom: 32px;
    z-index: 10000;
    display: none;
    box-sizing: border-box;
    max-width: min(480px, calc(100vw - 32px));
    padding: 10px 14px;
    border-radius: 10px;
    background: rgba(24, 24, 28, .92);
    color: #fff;
    font-size: 13px;
    line-height: 18px;
    overflow-wrap: anywhere;
    box-shadow: 0 10px 30px rgba(0, 0, 0, .24);
    transform: translateX(-50%);
    pointer-events: none;
  }
`;
function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement("style");
  style.id = STYLE_ID;
  style.textContent = STYLE_TEXT;
  (document.head || document.documentElement).appendChild(style);
}

// src/client/recent-dirs.ts
var STORAGE_KEY2 = "dsh-fnos-advanced:recent-dirs";
var MAX_RECENT_DIRS = 8;
function normalizePath(value) {
  const text = String(value ?? "");
  if (!text.startsWith("/")) return "";
  const trimmed = text.replace(/\/+$/, "");
  return trimmed || "/";
}
function pushRecentPath(list, path, max = MAX_RECENT_DIRS) {
  const value = normalizePath(path);
  if (!value || max <= 0) return list.slice(0, Math.max(0, max));
  const seen = /* @__PURE__ */ new Set([value]);
  const next = [value];
  for (const item of list) {
    const normalized = normalizePath(item);
    if (!normalized || seen.has(normalized)) continue;
    seen.add(normalized);
    next.push(normalized);
    if (next.length >= max) break;
  }
  return next;
}
function loadRecentPaths() {
  try {
    if (typeof window === "undefined") return [];
    const raw = window.localStorage.getItem(STORAGE_KEY2);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.map(normalizePath).filter(Boolean).slice(0, MAX_RECENT_DIRS);
  } catch {
    return [];
  }
}
function recordRecentPath(path) {
  const next = pushRecentPath(loadRecentPaths(), path);
  try {
    if (typeof window !== "undefined") {
      window.localStorage.setItem(STORAGE_KEY2, JSON.stringify(next));
    }
  } catch {
  }
  return next;
}
function clearRecentPaths() {
  try {
    if (typeof window !== "undefined") window.localStorage.removeItem(STORAGE_KEY2);
  } catch {
  }
}

// src/client/workspace-picker.ts
var statusTimers = /* @__PURE__ */ new WeakMap();
function scanWorkspacePickers() {
  for (const dialog of document.querySelectorAll('[role="dialog"]')) {
    if (!isWorkspacePicker(dialog)) {
      dialog.querySelectorAll(`[${BUTTON_ATTR}], .${STATUS_CLASS}, .dsh-fnos-recent`).forEach((node) => node.remove());
      continue;
    }
    const footer = findFooter(dialog);
    if (!footer) continue;
    const button = dialog.querySelector(`[${BUTTON_ATTR}]`);
    if (button) refreshButtonLabels(button);
    else injectButton(dialog, footer);
    renderRecentChips(dialog, footer);
  }
}
function isWorkspacePicker(dialog) {
  if (!isVisible(dialog)) return false;
  const title = [
    dialog.getAttribute("aria-label") || "",
    ...Array.from(dialog.querySelectorAll("h1, h2, h3")).map((node) => node.textContent || "")
  ].join(" ");
  if (!WORKSPACE_TITLE_RE.test(title)) return false;
  if (!findPathInput(dialog) && !findEditButton(dialog)) return false;
  return Boolean(findFooter(dialog));
}
function injectButton(dialog, footer) {
  const primary = document.createElement("button");
  primary.type = "button";
  primary.setAttribute(BUTTON_ATTR, "1");
  primary.dataset.pick = "directory";
  primary.className = "dsh-fnos-advanced-button";
  primary.textContent = t("选择飞牛目录");
  primary.title = t("通过飞牛授权选择一个目录（会授予当前飞牛应用访问权限）");
  footer.insertBefore(primary, footer.firstChild);
}
function refreshButtonLabels(button) {
  if (button.dataset.busy === "1") return;
  const label = t("选择飞牛目录");
  const hint = t("通过飞牛授权选择一个目录（会授予当前飞牛应用访问权限）");
  if (button.textContent !== label) button.textContent = label;
  if (button.title !== hint) button.title = hint;
}
function renderRecentChips(dialog, footer) {
  const paths = loadRecentPaths();
  let container = dialog.querySelector(".dsh-fnos-recent");
  if (!paths.length) {
    container?.remove();
    repositionStatus(dialog);
    return;
  }
  if (!container) {
    container = document.createElement("div");
    container.className = "dsh-fnos-recent";
  }
  if (footer.firstElementChild !== container) footer.insertBefore(container, footer.firstChild);
  const signature = `${currentLanguage()}
${paths.join("\n")}`;
  if (container.dataset.signature === signature) return;
  container.dataset.signature = signature;
  container.replaceChildren();
  const label = document.createElement("span");
  label.className = "dsh-fnos-recent-label";
  label.textContent = t("最近使用");
  container.append(label);
  for (const path of paths) {
    const chip = document.createElement("button");
    chip.type = "button";
    chip.className = "dsh-fnos-recent-chip";
    chip.textContent = fileNameForPath(path);
    chip.title = path;
    chip.setAttribute("aria-label", t("填入最近使用的目录：{path}", { path }));
    chip.addEventListener("mousedown", (event) => {
      event.preventDefault();
      event.stopPropagation();
    });
    chip.addEventListener("click", (event) => {
      event.preventDefault();
      event.stopPropagation();
      void useRecentPath(dialog, path);
    });
    container.append(chip);
  }
  const clear = document.createElement("button");
  clear.type = "button";
  clear.className = "dsh-fnos-recent-clear";
  clear.textContent = "×";
  clear.title = t("清除最近记录");
  clear.setAttribute("aria-label", t("清除最近记录"));
  clear.addEventListener("click", (event) => {
    event.preventDefault();
    event.stopPropagation();
    clearRecentPaths();
    renderRecentChips(dialog, footer);
  });
  container.append(clear);
  repositionStatus(dialog);
}
function repositionStatus(dialog) {
  const status = dialog.querySelector(`.${STATUS_CLASS}`);
  if (!status || status.style.display !== "block") return;
  const footer = findFooter(dialog);
  status.style.bottom = `${(footer instanceof HTMLElement ? footer.offsetHeight : 56) + 12}px`;
}
async function useRecentPath(dialog, path) {
  const input = await ensurePathInput(dialog);
  if (!input) {
    showStatus(dialog, t("没有找到目录选择器的路径输入框"), true);
    return;
  }
  fillAndEnter(input, path);
  recordRecentPath(path);
  showStatus(dialog, t("已填入路径"), false);
}
function findPathInput(dialog) {
  for (const input of dialog.querySelectorAll("input")) {
    if (input.type && input.type !== "text") continue;
    const label = [
      input.getAttribute("aria-label") || "",
      input.getAttribute("placeholder") || "",
      input.title || ""
    ].join(" ");
    if (PATH_LABEL_RE.test(label)) return input;
    if (typeof input.value === "string" && input.value.startsWith("/")) return input;
  }
  return null;
}
function findEditButton(dialog) {
  for (const button of dialog.querySelectorAll("button")) {
    const label = [
      button.getAttribute("aria-label") || "",
      button.title || "",
      button.textContent || ""
    ].join(" ");
    if (PATH_LABEL_RE.test(label)) return button;
  }
  const trail = dialog.querySelector('[role="navigation"]');
  const bar = trail && trail.parentElement;
  if (bar) {
    for (const button of bar.querySelectorAll("button")) {
      if (trail.contains(button)) continue;
      if (!(button.textContent || "").trim()) return button;
    }
  }
  return null;
}
function findFooter(dialog) {
  const buttons = Array.from(dialog.querySelectorAll("button"));
  const cancel = buttons.find((button) => CANCEL_BUTTON_RE.test(buttonText(button)));
  const open = buttons.find((button) => OPEN_BUTTON_RE.test(buttonText(button)));
  if (cancel && open && cancel.parentElement === open.parentElement) {
    return cancel.parentElement;
  }
  if (open && open.parentElement) return open.parentElement;
  if (cancel && cancel.parentElement) return cancel.parentElement;
  const last = buttons[buttons.length - 1];
  if (last && last.parentElement && last.parentElement !== dialog) return last.parentElement;
  return null;
}
function buttonText(button) {
  return (button.textContent || "").replace(/\s+/g, " ").trim();
}
async function ensurePathInput(dialog) {
  let input = findPathInput(dialog);
  if (input) return input;
  const edit = findEditButton(dialog);
  if (!edit) return null;
  edit.click();
  for (let i2 = 0; i2 < 24; i2 += 1) {
    await sleep(50);
    input = findPathInput(dialog);
    if (input) {
      input.focus();
      return input;
    }
  }
  return null;
}
async function handlePick(button) {
  const dialog = button.closest('[role="dialog"]');
  if (!dialog) return;
  await runPickFlow(button, directoryFlow(dialog, button));
}
function directoryFlow(dialog, button) {
  const initialText = button.textContent || t("选择飞牛目录");
  return {
    button,
    method: "pickUserFile",
    directory: true,
    creatable: true,
    title: t("选择工作区目录"),
    okText: t("确认授权"),
    anchor: dialog,
    status: (text, isError) => showStatus(dialog, text, isError === true),
    isDuplicate: async (paths) => isDuplicateWorkspacePath(paths[0], await knownWorkspacePaths()),
    onPaths: async (paths) => {
      const input = await ensurePathInput(dialog);
      if (!input) throw new Error(t("没有找到目录选择器的路径输入框"));
      fillAndEnter(input, paths[0]);
      recordRecentPath(paths[0]);
      const footer = findFooter(dialog);
      if (footer) renderRecentChips(dialog, footer);
    },
    doneText: (paths) => t("已填入授权目录：{path}", { path: paths[0] }),
    setLabel: (text) => {
      button.textContent = text;
    },
    doneLabel: () => t("已填入路径"),
    restore: () => {
      button.textContent = initialText;
      button.disabled = false;
      delete button.dataset.busy;
    }
  };
}
function showStatus(dialog, text, isError) {
  const host = dialog;
  if (getComputedStyle(host).position === "static") host.style.position = "relative";
  let status = dialog.querySelector(`.${STATUS_CLASS}`);
  if (!status) {
    status = document.createElement("div");
    status.className = STATUS_CLASS;
    status.setAttribute("role", "status");
    dialog.appendChild(status);
  }
  status.textContent = text;
  status.dataset.error = isError ? "1" : "0";
  status.style.display = "block";
  repositionStatus(dialog);
  const previous = statusTimers.get(status);
  if (previous) window.clearTimeout(previous);
  statusTimers.set(status, window.setTimeout(() => {
    status.style.display = "none";
  }, isError ? 1e4 : 6e3));
}

// src/client/index.ts
var installedDispose = null;
function apply(ctx) {
  if (typeof window === "undefined" || typeof document === "undefined") return;
  setHostLanguageResolver(() => getSdk().getPlatformConfig());
  if (getLanguagePreference() === "system") void resolveHostLanguage();
  const dispose = install();
  ctx?.effect?.(() => dispose, "dsh-fnos-advanced: workspace directory picker");
  registerSettingsSlot(ctx);
  registerPathActionSlots(ctx);
  registerSessionCwd(ctx);
  registerWorkspacePaths(ctx);
}
function install() {
  if (installedDispose) return installedDispose;
  injectStyle();
  ensureHostPathBridge();
  const cleanups = [];
  let scanTimer = 0;
  const scheduleScan = () => {
    if (scanTimer) return;
    scanTimer = window.setTimeout(() => {
      scanTimer = 0;
      scan();
    }, 80);
  };
  const observer = new MutationObserver(scheduleScan);
  observer.observe(document.documentElement, { childList: true, subtree: true });
  cleanups.push(() => observer.disconnect());
  cleanups.push(onLanguageChange(() => {
    scan();
  }));
  const onClick = (event) => {
    const target = event.target instanceof Element ? event.target.closest(`[${BUTTON_ATTR}]`) : null;
    if (!target) return;
    event.preventDefault();
    event.stopPropagation();
    void handlePick(target);
  };
  document.addEventListener("click", onClick, true);
  cleanups.push(() => document.removeEventListener("click", onClick, true));
  const onWorkspaceRowClick = (event) => {
    const row = event.target instanceof Element ? event.target.closest('[data-row-key^="workspace:"]') : null;
    if (!row) return;
    noteWorkspaceRowClick(row);
    scheduleScan();
  };
  document.addEventListener("click", onWorkspaceRowClick, true);
  cleanups.push(() => document.removeEventListener("click", onWorkspaceRowClick, true));
  getSdk().ready().then(scheduleScan).catch(() => {
  });
  const dispose = () => {
    if (scanTimer) {
      window.clearTimeout(scanTimer);
      scanTimer = 0;
    }
    for (const cleanup of cleanups.splice(0)) {
      try {
        cleanup();
      } catch {
      }
    }
    closePathMenu();
    for (const node of document.querySelectorAll(
      `[${BUTTON_ATTR}], [${PICK_ITEM_ATTR}], .${STATUS_CLASS}, .dsh-fnos-recent, [data-dsh-fnos-open-dir], [data-dsh-fnos-check-dir], [data-dsh-fnos-reauth-dir]`
    )) {
      node.remove();
    }
    for (const row of document.querySelectorAll(".dsh-fnos-file-row")) {
      row.classList.remove("dsh-fnos-file-row");
      if (row.dataset.dshFnosPosition === "1") {
        row.style.position = "";
        delete row.dataset.dshFnosPosition;
      }
    }
    for (const node of document.querySelectorAll(`[${FILE_ACTION_ATTR}]`)) node.remove();
    const style = document.getElementById(STYLE_ID);
    if (style) style.remove();
    installedDispose = null;
  };
  installedDispose = dispose;
  scan();
  return dispose;
}
function scan() {
  scanWorkspacePickers();
  scanCommandMenu();
  scanReferenceMenu();
  scanFileTree();
  scanWorkspaceMenus();
  recordAnchorObservations();
}
function registerWorkspacePaths(ctx) {
  const inject = ctx?.inject;
  if (typeof inject !== "function") return;
  inject(["workspaces"], (scope) => {
    const list = scope.workspaces?.list;
    if (!list || typeof list.getSnapshot !== "function") return;
    const update = () => {
      try {
        const snapshot = list.getSnapshot?.();
        const items = Array.isArray(snapshot?.items) ? snapshot.items : [];
        const entries = items.filter((item) => Boolean(item) && item.workspaceId !== null && item.workspaceId !== void 0 && typeof item.path === "string").map((item) => [String(item.workspaceId), item.path]);
        setWorkspacePaths(entries);
      } catch {
      }
    };
    update();
    if (typeof list.subscribe === "function" && typeof scope.effect === "function") {
      try {
        scope.effect(() => list.subscribe?.(update), "dsh-fnos-advanced: workspace paths");
      } catch {
      }
    }
  });
}
if (typeof window !== "undefined") {
  try {
    ensureHostPathBridge();
  } catch {
  }
}
if (typeof window !== "undefined") {
  window.__dshFnosAdvanced = {
    version: VERSION,
    install,
    handlePick,
    isPickerCancelled,
    isCancellationText,
    pickUserFile: (options) => getSdk().pickUserFile(options),
    openFileManager: (path) => getSdk().openFileManager(path),
    loadSettings,
    resolveAuthTargets,
    resolveAuthBase,
    findPathInput,
    findEditButton,
    findFooter,
    isWorkspacePicker,
    fillAndEnter,
    findComposerFileInput,
    findActiveComposerInput,
    addPathsToComposer,
    ensureHostPathBridge,
    getSdk,
    pickPathsDirect,
    isDuplicateWorkspacePath
  };
}

    return module.exports;
  },
  });
})();
