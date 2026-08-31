import { ref, type Ref } from 'vue'

export type Locale = 'zh' | 'en'

// 语言偏好只保存在本浏览器（localStorage），不随设置持久化到后端。
const LOCALE_KEY = 'console-language'

function readStoredLocale(): Locale {
  const v = localStorage.getItem(LOCALE_KEY)
  return isLocale(v || '') ? (v as Locale) : 'zh'
}

// 当前语言（模块级共享，reactive）
const locale = ref<Locale>(readStoredLocale()) as Ref<Locale>

export function isLocale(v: string): v is Locale {
  return v === 'zh' || v === 'en'
}

const zh: Record<string, string> = {
  // 导航
  nav_overview: '概览',
  nav_settings: '设置',
  nav_directory: '资源',
  nav_terminal: '终端',
  nav_logs: '日志',
  nav_theme: '主题',
  sidebar_expand: '展开',
  sidebar_collapse: '折叠',
  theme_label: '主题',
  theme_light: '浅色',
  theme_dark: '深色',
  theme_system: '跟随系统',

  // 设置页
  settings_title: '设置',
  settings_save: '保存配置',
  settings_desc: '配置代理、端口与登录鉴权。',
  settings_enable_proxy: '启用代理',
  settings_proxy_addr: '代理地址',
  settings_proxy_hint: '可填写http代理，例如 http://127.0.0.1:7890，socks5代理仅为实验性',
  settings_dsh_port: 'dsh 端口',
  settings_enable_auth: '启用登录鉴权',
  settings_password: '访问密码',
  settings_password_hint: '(≥8位，含字母/数字/标点)',
  settings_password_placeholder: '留空则鉴权不生效',
  settings_auth_ttl: '登录有效期',
  settings_auth_ttl_hour: '(小时)',
  settings_auth_ttl_hint: '登录鉴权默认有效期 2 小时，超过后将要求重新登录',
  settings_dsh_mem_limit: 'dsh 内存限制',
  settings_dsh_mem_mb: '(MB)',
  settings_dsh_mem_hint: '仅在 node 栈内存溢出时适当增大',
  settings_show_password: '显示密码',
  settings_hide_password: '隐藏密码',
  // 控制台设置
  console_title: '控制台设置',
  console_theme: '主题',
  console_language: '语言',
  console_default_page: '打开时的默认页面',
  lang_zh: '中文',
  lang_en: 'English',
  default_overview: '默认概览',
  default_last: '保持退出时的页面',
  default_settings: '设置页',
  default_directory: '资源页',
  default_terminal: '终端页',
  default_logs: '日志页',
  saved: '配置已保存',
  saved_proxy_restart: '配置已保存，代理设置已变更，请重启 dsh 服务使配置生效',

  // 概览页
  overview_title: '概览',
  dsh_start: '启动',
  dsh_stop: '停止',
  dsh_restart: '重启',
  status_running: '运行中',
  status_stopped: '已停止',
  cpu_usage: 'CPU 使用率',
  mem_usage: '内存占用',
  login_list: '登录列表',
  login_list_desc: '注销后需重新登录',
  logout: '注销',
  logging_out: '注销中…',
  last_access: '最近访问',
  expires_at: '登录有效期至',
  no_visitors: '暂无登录记录',
  loading: '加载中…',
  dsh_started: 'dsh 已启动',
  dsh_stopped: 'dsh 已停止',
  dsh_restarted: 'dsh 已重启',
  // 停止/重启前的二次确认
  confirm_stop_title: '停止 dsh 服务',
  confirm_restart_title: '重启 dsh 服务',
  confirm_stop_msg: '确定要停止 dsh 服务吗？',
  confirm_restart_msg: '确定要重启 dsh 服务吗？',
  confirm_ok: '确定',
  confirm_cancel: '取消',

  // 资源页
  directory_title: '资源',
  directory_add: '添加',
  directory_loading: '加载中…',
  directory_empty: '暂无已授权的目录',
  directory_empty_desc: '点击添加按钮授权你的飞牛目录。',
  directory_open: '打开',
  directory_remove: '移除',
  directory_auth_success: '授权成功',
  directory_auth_cancel: '已取消授权',
  directory_auth_updated: '授权目录已更新',
  directory_appid_missing: '无法获取应用标识，请稍后重试',
  directory_pick_title: '选择授权目录',
  directory_pick_ok: '确认授权',
  directory_open_window: '已打开授权窗口，完成选择后将自动刷新',
  directory_open_failed: '打开文件管理器失败: {msg}',

  // 插件管理
  plugin_title: '插件管理',
  plugin_desc: 'dsh启动失败时可卸载不兼容插件或重置清除所有插件。',
  plugin_refresh: '刷新',
  plugin_reset: '重置',
  plugin_loading: '加载中…',
  plugin_empty: '暂无插件',
  plugin_uninstall: '卸载',
  plugin_removing: '卸载中…',
  plugin_removed: '已卸载 {name}',
  plugin_remove_failed: '卸载失败',
  plugin_reset_done: '已重置全部插件',
  plugin_reset_partial: '有 {n} 个插件重置失败',

  // 日志页
  logs_title: '日志',
  log_auto_scroll_on: '自动滚动：开',
  log_auto_scroll_off: '自动滚动：关',
  log_pause_scroll: '暂停自动滚动',
  log_resume_scroll: '恢复自动滚动',
  log_empty: '暂无日志内容',
  log_no_file: '（未配置日志文件，未设置 HARNESS_LOG_FILE）',
  log_export: '导出',
  log_export_failed: '导出失败',
  log_not_configured: '日志文件未配置，无法导出',

  // 终端页
  terminal_title: '终端',
  term_reconnect: '重连',
  term_clear: '清空',
  term_copy: '复制',
  term_paste: '粘贴',
  term_copied: '已复制到剪贴板',
  term_no_selection: '没有选中内容',
  term_select_hint: '请直接框选文本复制',
  term_not_connected: '终端未连接',
  term_reconnected: '已重新连接',
  term_conn_closed: '连接已关闭。刷新页面重连。',
  term_ws_error: 'WebSocket 错误。',
}

const en: Record<string, string> = {
  nav_overview: 'Overview',
  nav_settings: 'Settings',
  nav_directory: 'Resources',
  nav_terminal: 'Terminal',
  nav_logs: 'Logs',
  nav_theme: 'Theme',
  sidebar_expand: 'Expand',
  sidebar_collapse: 'Collapse',
  theme_label: 'Theme',
  theme_light: 'Light',
  theme_dark: 'Dark',
  theme_system: 'System',

  settings_title: 'Settings',
  settings_save: 'Save',
  settings_desc: 'Configure proxy, port and login authentication.',
  settings_enable_proxy: 'Enable proxy',
  settings_proxy_addr: 'Proxy address',
  settings_proxy_hint: 'e.g. http://127.0.0.1:7890 (socks5 is experimental)',
  settings_dsh_port: 'dsh port',
  settings_enable_auth: 'Enable login auth',
  settings_password: 'Access password',
  settings_password_hint: '(≥8 chars, letters/numbers/punctuation)',
  settings_password_placeholder: 'Leave empty to disable auth',
  settings_auth_ttl: 'Login validity',
  settings_auth_ttl_hour: '(hours)',
  settings_auth_ttl_hint: 'Default 2 hours; re-login required after it expires',
  settings_dsh_mem_limit: 'dsh memory limit',
  settings_dsh_mem_mb: '(MB)',
  settings_dsh_mem_hint: 'Increase only when node stack runs out of memory',
  settings_show_password: 'Show password',
  settings_hide_password: 'Hide password',
  console_title: 'Console Settings',
  console_theme: 'Theme',
  console_language: 'Language',
  console_default_page: 'Default page on open',
  lang_zh: '中文',
  lang_en: 'English',
  default_overview: 'Overview',
  default_last: 'Last viewed page',
  default_settings: 'Settings',
  default_directory: 'Resources',
  default_terminal: 'Terminal',
  default_logs: 'Logs',
  saved: 'Settings saved',
  saved_proxy_restart: 'Saved. Proxy changed — restart dsh to apply.',

  overview_title: 'Overview',
  dsh_start: 'Start',
  dsh_stop: 'Stop',
  dsh_restart: 'Restart',
  status_running: 'Running',
  status_stopped: 'Stopped',
  cpu_usage: 'CPU usage',
  mem_usage: 'Memory',
  login_list: 'Login list',
  login_list_desc: 'Log out to re-authenticate',
  logout: 'Log out',
  logging_out: 'Logging out…',
  last_access: 'Last access',
  expires_at: 'Login expires at',
  no_visitors: 'No login records',
  loading: 'Loading…',
  dsh_started: 'dsh started',
  dsh_stopped: 'dsh stopped',
  dsh_restarted: 'dsh restarted',
  // Lifecycle confirmation before stop/restart
  confirm_stop_title: 'Stop dsh service',
  confirm_restart_title: 'Restart dsh service',
  confirm_stop_msg: 'Are you sure you want to stop the dsh service?',
  confirm_restart_msg: 'Are you sure you want to restart the dsh service?',
  confirm_ok: 'Confirm',
  confirm_cancel: 'Cancel',

  // 资源页
  directory_title: 'Resources',
  directory_add: 'Add',
  directory_loading: 'Loading…',
  directory_empty: 'No authorized directories',
  directory_empty_desc: 'Click Add to authorize your fnOS directory.',
  directory_open: 'Open',
  directory_remove: 'Remove',
  directory_auth_success: 'Authorization succeeded',
  directory_auth_cancel: 'Authorization canceled',
  directory_auth_updated: 'Authorized directories updated',
  directory_appid_missing: 'Unable to get app ID, please try again',
  directory_pick_title: 'Select directory to authorize',
  directory_pick_ok: 'Confirm authorization',
  directory_open_window: 'Authorization window opened, refreshing after selection',
  directory_open_failed: 'Failed to open file manager: {msg}',

  // 插件管理
  plugin_title: 'Plugins',
  plugin_desc: 'Uninstall incompatible plugins or reset to clear all plugins when dsh fails to start.',
  plugin_refresh: 'Refresh',
  plugin_reset: 'Reset',
  plugin_loading: 'Loading…',
  plugin_empty: 'No plugins',
  plugin_uninstall: 'Uninstall',
  plugin_removing: 'Uninstalling…',
  plugin_removed: 'Removed {name}',
  plugin_remove_failed: 'Remove failed',
  plugin_reset_done: 'All plugins reset',
  plugin_reset_partial: '{n} plugin(s) failed to reset',

  logs_title: 'Logs',
  log_auto_scroll_on: 'Auto-scroll: on',
  log_auto_scroll_off: 'Auto-scroll: off',
  log_pause_scroll: 'Pause auto-scroll',
  log_resume_scroll: 'Resume auto-scroll',
  log_empty: 'No log content',
  log_no_file: '(No log file configured, HARNESS_LOG_FILE not set)',
  log_export: 'Export',
  log_export_failed: 'Export failed',
  log_not_configured: 'Log file not configured, cannot export',

  terminal_title: 'Terminal',
  term_reconnect: 'Reconnect',
  term_clear: 'Clear',
  term_copy: 'Copy',
  term_paste: 'Paste',
  term_copied: 'Copied to clipboard',
  term_no_selection: 'Nothing selected',
  term_select_hint: 'Select text to copy',
  term_not_connected: 'Terminal not connected',
  term_reconnected: 'Reconnected',
  term_conn_closed: 'Connection closed. Refresh to reconnect.',
  term_ws_error: 'WebSocket error.',
}

const dict: Record<Locale, Record<string, string>> = { zh, en }

export function setLocale(l: Locale) {
  locale.value = l
  localStorage.setItem(LOCALE_KEY, l)
}

export function useI18n() {
  function t(key: string, params?: Record<string, string | number>): string {
    let s = dict[locale.value][key]
    if (s === undefined) s = dict.zh[key]
    if (s === undefined) s = key
    if (params) {
      for (const k of Object.keys(params)) {
        s = s.replace(new RegExp(`\\{${k}\\}`, 'g'), String(params[k]))
      }
    }
    return s
  }
  return { locale, t, setLocale }
}