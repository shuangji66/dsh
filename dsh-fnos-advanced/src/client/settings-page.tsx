/**
 * Plugin-center settings page.
 *
 * Registered on dsh's `plugins.bundle.config` slot so it renders directly on the
 * package page. Only `react` is imported (from the loader module table); all
 * styling is inline with dsh's `--dsw-alias-*` custom properties and neutral
 * fallbacks, so the page follows the active theme without shipping CSS that
 * would depend on dsh's CSS-module class names.
 */
import * as React from 'react';
import { DEFAULT_SETTINGS, normalizeBase, type AuthMode } from '../shared/settings.ts';
import { VERSION } from '../version.ts';
import { openAuthPopup } from './auth.ts';
import { closePopup, copyText, showToast } from './dom.ts';
import { currentLanguage, getLanguagePreference, onLanguageChange, resolveHostLanguage, setLanguagePreference, t, translateError, type LanguagePreference } from './i18n.ts';
import {
  authRouteExists,
  checkWorkspaceHealth,
  fetchPluginHealth,
  getCachedSettings,
  getSettingsHostState,
  lanBaseFromSettings,
  loadConfig,
  loadSettings,
  normalizeClientSettings,
  saveSettings,
  workspaceHealthText,
  type PluginConfig,
  type PluginHealth,
  type SettingsHostState,
  type WorkspaceHealthRow,
} from './plugin-api.ts';
import { isFnosAppEmbedded } from './env.ts';
import { getSdk, isFnosHost } from './sdk.ts';
import { authorizeWorkspacePath } from './workspace-menu.ts';

const hintStyle: React.CSSProperties = {
  margin: 0,
  fontSize: 12,
  lineHeight: '18px',
  color: 'var(--dsw-alias-label-tertiary, #888)',
};

const errorHintStyle: React.CSSProperties = {
  ...hintStyle,
  color: 'var(--dsw-alias-state-error-primary, #d93025)',
};

const inputStyle: React.CSSProperties = {
  boxSizing: 'border-box',
  width: '100%',
  height: 36,
  padding: '0 10px',
  borderRadius: 8,
  border: '1px solid var(--dsw-alias-border-l2, rgba(127,127,127,.32))',
  background: 'transparent',
  color: 'var(--dsw-alias-label-primary, inherit)',
  font: 'inherit',
  fontSize: 14,
  outline: 'none',
};

const labelStyle: React.CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 6,
  fontSize: 13,
  color: 'var(--dsw-alias-label-secondary, #666)',
};

const smallButtonStyle: React.CSSProperties = {
  boxSizing: 'border-box',
  height: 32,
  padding: '0 12px',
  borderRadius: 8,
  border: '1px solid var(--dsw-alias-border-l2, rgba(127,127,127,.35))',
  background: 'transparent',
  color: 'var(--dsw-alias-label-primary, inherit)',
  font: 'inherit',
  fontSize: 13,
  cursor: 'pointer',
};

/** Slot entry: the package page renders a one-line summary, the page renders the form. */
export function SettingsSlot(props: { view?: string }): React.ReactNode {
  if (props && props.view === 'summary') return settingsSummary();
  return <SettingsPage />;
}

function settingsSummary(): string {
  const settings = getCachedSettings();
  const proxy = settings.proxyBase || t('未设置');
  return t('反代 {proxy} · 内网 {lan}', { proxy, lan: settings.lanTarget });
}

interface CollapsibleSectionProps {
  title: string;
  description?: string;
  defaultOpen?: boolean;
  children?: React.ReactNode;
}

function CollapsibleSection({ title, description, defaultOpen, children }: CollapsibleSectionProps): React.ReactElement {
  const [open, setOpen] = React.useState(defaultOpen !== false);

  return (
    <section style={{
      display: 'flex',
      flexDirection: 'column',
      gap: 8,
      paddingTop: 10,
      borderTop: '1px solid var(--dsw-alias-border-l3, rgba(127,127,127,.22))',
    }}>
      <button
        type="button"
        aria-expanded={open}
        aria-label={`${title} · ${open ? t('收起') : t('展开')}`}
        onClick={() => setOpen((value) => !value)}
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 12,
          width: '100%',
          padding: '2px 0',
          border: 'none',
          background: 'transparent',
          color: 'inherit',
          font: 'inherit',
          textAlign: 'left',
          cursor: 'pointer',
        }}
      >
        <span style={{ fontSize: 14, fontWeight: 600 }}>{title}</span>
        <span aria-hidden style={{
          color: 'var(--dsw-alias-label-tertiary, #888)',
          fontSize: 14,
          transform: open ? 'rotate(90deg)' : 'none',
          transition: 'transform .12s ease',
        }}>›</span>
      </button>
      {description ? <p style={{ ...hintStyle, display: open ? 'block' : 'none' }}>{description}</p> : null}
      <div style={{ display: open ? 'block' : 'none' }}>{children}</div>
    </section>
  );
}

function SettingsPage(): React.ReactElement {
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);
  const [settings, setSettings] = React.useState(() => ({ ...DEFAULT_SETTINGS }));
  const [message, setMessage] = React.useState('');
  const [error, setError] = React.useState('');
  const [embedded, setEmbedded] = React.useState(false);
  const [gatewayAccess, setGatewayAccess] = React.useState(/^\/app(?:\/|$)/.test(window.location.pathname));
  const [hostState, setHostState] = React.useState<SettingsHostState>('unknown');
  const [hostVersion, setHostVersion] = React.useState('');
  const [language, setLanguage] = React.useState<LanguagePreference>(getLanguagePreference());
  const [, setLanguageVersion] = React.useState(0);

  // "Follow fnOS" resolves asynchronously; re-render once the host answers.
  React.useEffect(() => {
    const unsubscribe = onLanguageChange(() => setLanguageVersion((value) => value + 1));
    if (getLanguagePreference() === 'system') void resolveHostLanguage();
    return unsubscribe;
  }, []);

  React.useEffect(() => {
    let alive = true;
    loadSettings(true).then((loaded) => {
      if (!alive) return;
      setSettings(loaded);
      setHostState(getSettingsHostState());
      setLoading(false);
    }).catch(() => {
      if (alive) setLoading(false);
    });

    loadConfig().then((config: PluginConfig) => {
      if (alive) setHostVersion(config.version || '');
    }).catch(() => { /* keep empty */ });

    const sdk = getSdk();
    sdk.ready().then(() => {
      if (alive) setEmbedded(!sdk.isStandaloneWeb);
    }).catch(() => { /* no host bridge */ });

    if (!/^\/app(?:\/|$)/.test(window.location.pathname)) {
      loadConfig()
        .then((config) => authRouteExists(config.appName || 'Harness'))
        .then((ok) => { if (alive) setGatewayAccess(ok); })
        .catch(() => { /* keep path-based result */ });
    }

    return () => { alive = false; };
  }, []);

  const update = (key: 'proxyBase' | 'lanTarget' | 'authMode') => (
    event: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>,
  ) => {
    const value = event.target.value;
    setSettings((current) => ({
      ...current,
      [key]: key === 'authMode' ? value as AuthMode : value,
    }));
    setMessage('');
    setError('');
  };

  const save = async () => {
    setSaving(true);
    setMessage('');
    setError('');
    try {
      const saved = await saveSettings(settings);
      setSettings(normalizeClientSettings(saved));
      setMessage(t('已保存'));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const hostOutdated = Boolean(hostVersion && hostVersion !== VERSION);
  const hostReady = hostState === 'ok' && !hostOutdated;
  const hostWarning = hostReady
    ? ''
    : hostOutdated
      ? t('宿主半侧版本 {host}，客户端 {client}。请重启 dsh 后再保存设置。', { host: hostVersion, client: VERSION })
      : hostState === 'missing'
        ? t('宿主半侧未提供设置接口，通常是插件升级后没有重启 dsh。请重启 dsh 后再保存设置。')
        : hostState === 'error'
          ? t('无法读取宿主设置接口，请确认 dsh 运行正常。')
          : '';
  const disabled = loading || saving || !hostReady;
  const buttonStyle: React.CSSProperties = {
    boxSizing: 'border-box',
    height: 36,
    padding: '0 16px',
    borderRadius: 8,
    border: '1px solid var(--dsw-alias-border-l2, rgba(127,127,127,.35))',
    background: 'transparent',
    color: 'var(--dsw-alias-label-primary, inherit)',
    font: 'inherit',
    fontSize: 14,
    fontWeight: 500,
    cursor: disabled ? 'default' : 'pointer',
    opacity: disabled ? 0.6 : 1,
  };

  const accessNote = embedded
    ? t('当前运行在飞牛桌面/宿主内：会直接调用飞牛目录选择器，不打开网页授权。')
    : gatewayAccess
      ? t('当前通过飞牛网关访问：直接使用当前同源地址授权，不会弹出入口选择。')
      : t('当前不是通过飞牛网关访问：同时配置反代与内网地址后，点击「选择飞牛目录」会让你选择授权入口。');

  return (
    <div style={{
      display: 'flex',
      flexDirection: 'column',
      gap: 14,
      padding: '4px 0 10px',
      maxWidth: 640,
      color: 'var(--dsw-alias-label-primary, inherit)',
      fontSize: 14,
    }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <div style={{ fontSize: 15, fontWeight: 600 }}>{t('飞牛目录授权')}</div>
        <p style={hintStyle}>{t('配置非网关访问时使用的飞牛授权入口；设置保存在当前 dsh 数据目录。')}</p>
      </div>

      <CollapsibleSection
        title={t('连接与授权')}
        description={t('配置反代/内网授权入口、界面语言和图片读取。')}
        defaultOpen
      >
        <label style={labelStyle}>
          <span>{t('反代飞牛地址')}</span>
          <input
            type="text"
            value={settings.proxyBase}
            onChange={update('proxyBase')}
            placeholder="https://fnos.example.com"
            autoComplete="off"
            spellCheck={false}
            disabled={disabled}
            style={inputStyle}
          />
          <span style={hintStyle}>{t('填协议 + 域名[:端口]，不要带路径；留空表示未配置。')}</span>
        </label>

        <label style={labelStyle}>
          <span>{t('飞牛内网地址 / 端口')}</span>
          <input
            type="text"
            value={settings.lanTarget}
            onChange={update('lanTarget')}
            placeholder="5666 或 http://192.168.1.10:5666"
            autoComplete="off"
            spellCheck={false}
            disabled={disabled}
            style={inputStyle}
          />
          <span style={hintStyle}>{t('只填端口时使用当前访问主机的地址；默认 5666。')}</span>
        </label>

        <label style={labelStyle}>
          <span>{t('非网关访问时的授权入口')}</span>
          <select value={settings.authMode} onChange={update('authMode')} disabled={disabled} style={inputStyle}>
            <option value="ask">{t('每次询问（推荐）')}</option>
            <option value="proxy">{t('优先使用反代地址')}</option>
            <option value="lan">{t('优先使用内网地址')}</option>
          </select>
        </label>

        <label style={labelStyle}>
          <span>{t('界面语言')}</span>
          <select
            value={language}
            onChange={(event) => {
              const next = setLanguagePreference(event.target.value);
              setLanguage(next);
              if (next === 'system') void resolveHostLanguage(true);
              setMessage('');
              setError('');
            }}
            disabled={disabled}
            style={inputStyle}
          >
            <option value="system">{t('跟随系统')}</option>
            <option value="zh">{t('中文')}</option>
            <option value="en">English</option>
          </select>
        </label>

        <label style={{ ...labelStyle, flexDirection: 'row', alignItems: 'flex-start', gap: 8 }}>
          <input
            type="checkbox"
            checked={settings.imageRead !== false}
            onChange={(event) => {
              const checked = event.target.checked;
              setSettings((current) => ({ ...current, imageRead: checked }));
              setMessage('');
              setError('');
            }}
            disabled={disabled}
            style={{ marginTop: 3 }}
          />
          <span style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <span>{t('从飞牛选择图片时读取图片内容')}</span>
            <span style={hintStyle}>{t('图片需要字节才能被模型「看到」；关闭后图片也只生成路径引用。仅支持 PNG/JPEG/WebP/GIF，宿主只读取你选中的文件。')}</span>
          </span>
        </label>

        <p style={hintStyle}>{accessNote}</p>
        {hostWarning ? <p style={errorHintStyle}>{hostWarning}</p> : null}

        <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
          <button type="button" onClick={save} disabled={disabled} style={buttonStyle}>
            {saving ? t('保存中…') : t('保存')}
          </button>
          {loading ? <span style={hintStyle}>{t('读取设置…')}</span> : null}
          {message ? <span style={{ fontSize: 12, color: 'var(--dsw-alias-state-success-primary, #188038)' }}>{message}</span> : null}
          {error ? <span style={{ fontSize: 12, color: 'var(--dsw-alias-state-error-primary, #d93025)' }}>{error}</span> : null}
        </div>
      </CollapsibleSection>

      <CollapsibleSection
        title={t('工作区目录权限')}
        description={t('检查 dsh 工作区目录的读写权限。飞牛 ACL 记录损坏或目录移动后，dsh 会读不到这些目录。')}
        defaultOpen
      >
        <WorkspaceHealthSection disabled={disabled} />
      </CollapsibleSection>

      <CollapsibleSection
        title={t('诊断')}
        description={t('检查 dsh 版本、DOM 锚点和飞牛地址连通性。')}
        defaultOpen={false}
      >
        <DiagnosticsSection
          settings={settings}
          hostVersion={hostVersion}
          hostState={hostState}
          embedded={embedded}
          gatewayAccess={gatewayAccess}
        />
      </CollapsibleSection>
    </div>
  );
}

interface AddressStatus {
  pending?: true;
  ok?: boolean;
  ms?: number;
  reason?: 'notSet' | 'timeout' | 'unreachable';
}

interface DiagnosticsProps {
  settings: ReturnType<typeof getCachedSettings>;
  hostVersion: string;
  hostState: SettingsHostState;
  embedded: boolean;
  gatewayAccess: boolean;
}

function DiagnosticsSection({ settings, hostVersion, hostState, embedded, gatewayAccess }: DiagnosticsProps): React.ReactElement {
  const [proxyStatus, setProxyStatus] = React.useState<AddressStatus | null>(null);
  const [lanStatus, setLanStatus] = React.useState<AddressStatus | null>(null);
  const [copyMessage, setCopyMessage] = React.useState<boolean | null>(null);
  const [health, setHealth] = React.useState<PluginHealth | null>(null);
  const [tick, setTick] = React.useState(0);
  const [appName, setAppName] = React.useState('Harness');

  const loadHealth = React.useCallback(() => {
    fetchPluginHealth()
      .then((data) => setHealth(data))
      .catch(() => setHealth(null));
  }, []);

  React.useEffect(() => {
    let alive = true;
    loadConfig().then((config) => {
      if (alive && config.appName) setAppName(config.appName);
    }).catch(() => { /* keep default */ });
    loadHealth();
    return () => { alive = false; };
  }, [loadHealth]);

  const statusStyle = (status: AddressStatus): React.CSSProperties => ({
    fontSize: 12,
    color: status && status.ok === true
      ? 'var(--dsw-alias-state-success-primary, #188038)'
      : status && status.ok === false
        ? 'var(--dsw-alias-state-error-primary, #d93025)'
        : 'var(--dsw-alias-label-tertiary, #888)',
  });
  const statusText = (status: AddressStatus | null): string => {
    if (!status) return '';
    if (status.reason === 'notSet') return t('未设置');
    if (status.reason === 'timeout') return t('连接超时');
    if (status.reason === 'unreachable') return t('无法连接');
    return t('可连接（{ms}ms）', { ms: status.ms ?? 0 });
  };

  // Recompute anchors on every render (the sidebar/tab may have opened since).
  const composerRegion = document.querySelector('[data-conversation-region="composer"]');
  const composerAnchor = composerRegion
    ? Boolean(composerRegion.querySelector('input[type="file"][multiple]'))
    : null;
  const treeRoot = document.querySelector('[data-files-root]');
  const fileTreeAnchor = treeRoot ? Boolean(treeRoot.querySelector('[data-files-entry][data-files-path]')) : null;
  const anchorText = (value: boolean | null): string => (
    value === true ? t('可用') : value === false ? t('不可用') : t('未检测')
  );

  const testAddress = async (kind: 'proxy' | 'lan') => {
    const setStatus = kind === 'proxy' ? setProxyStatus : setLanStatus;
    const base = kind === 'proxy'
      ? normalizeBase(settings.proxyBase || '')
      : lanBaseFromSettings(settings);
    if (!base) {
      setStatus({ reason: 'notSet' });
      return;
    }
    setStatus({ pending: true });
    const controller = typeof AbortController === 'function' ? new AbortController() : null;
    const timer = controller ? window.setTimeout(() => controller.abort(), 4000) : 0;
    const started = Date.now();
    try {
      await fetch(
        `${base}/app-auth/pick-user-file?appName=${encodeURIComponent(appName)}&directory=true`,
        {
          mode: 'no-cors',
          cache: 'no-store',
          credentials: 'omit',
          signal: controller ? controller.signal : undefined,
        },
      );
      setStatus({ ok: true, ms: Date.now() - started });
    } catch (error) {
      setStatus({
        ok: false,
        reason: error instanceof Error && error.name === 'AbortError' ? 'timeout' : 'unreachable',
      });
    } finally {
      if (timer) window.clearTimeout(timer);
    }
  };

  const runtime = embedded ? t('飞牛桌面 / App') : gatewayAccess ? t('飞牛网关') : t('普通浏览器');
  const hostStateText = hostState === 'ok'
    ? t('正常')
    : hostState === 'missing'
      ? t('缺失（需重启 dsh）')
      : hostState === 'error' ? t('读取失败') : t('未检测');
  const imageReadText = health && health.features
    ? (health.features.imageRead ? t('开') : t('关'))
    : t('未检测');
  const workspaceHealthTextValue = health && health.features
    ? (health.features.workspaceHealth ? t('可用') : t('不可用'))
    : t('未检测');

  const copyDiagnostics = async () => {
    const lines = [
      `dsh-fnos-advanced ${VERSION}`,
      `${t('客户端')}: ${VERSION}`,
      `${t('宿主')}: ${hostVersion || '?'} (${hostStateText})`,
      `${t('运行环境')}: ${runtime}`,
      `${t('界面语言')}: ${currentLanguage()}`,
      `${t('对话输入框')}: ${anchorText(composerAnchor)}`,
      `${t('文件树')}: ${anchorText(fileTreeAnchor)}`,
      `${t('图片读取')}: ${imageReadText}`,
      `${t('工作区体检')}: ${workspaceHealthTextValue}`,
      `${t('反代飞牛地址')}: ${settings.proxyBase || t('未设置')}`,
      `${t('飞牛内网地址 / 端口')}: ${settings.lanTarget}`,
      `${t('测试反代地址')}: ${statusText(proxyStatus) || t('未检测')}`,
      `${t('测试内网地址')}: ${statusText(lanStatus) || t('未检测')}`,
    ];
    const ok = await copyText(lines.join('\n'));
    setCopyMessage(Boolean(ok));
  };

  return (
    <div key={`diagnostics-${tick}`} style={{ display: 'flex', flexDirection: 'column', gap: 10, paddingTop: 4 }}>
      <div style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '4px 12px', fontSize: 12 }}>
        <span style={hintStyle}>{t('宿主')}</span><span>{`${hostVersion || '?'} · ${hostStateText}`}</span>
        <span style={hintStyle}>{t('客户端')}</span><span>{VERSION}</span>
        <span style={hintStyle}>{t('运行环境')}</span><span>{runtime}</span>
        <span style={hintStyle}>{t('图片读取')}</span><span>{imageReadText}</span>
        <span style={hintStyle}>{t('对话输入框')}</span><span>{anchorText(composerAnchor)}</span>
        <span style={hintStyle}>{t('文件树')}</span><span>{anchorText(fileTreeAnchor)}</span>
        <span style={hintStyle}>{t('工作区体检')}</span><span>{workspaceHealthTextValue}</span>
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
        <button type="button" onClick={() => { void testAddress('proxy'); }} style={smallButtonStyle}>{t('测试反代地址')}</button>
        {proxyStatus
          ? <span style={statusStyle(proxyStatus)}>{proxyStatus.pending ? t('测试中…') : statusText(proxyStatus)}</span>
          : null}
        <button type="button" onClick={() => { void testAddress('lan'); }} style={smallButtonStyle}>{t('测试内网地址')}</button>
        {lanStatus
          ? <span style={statusStyle(lanStatus)}>{lanStatus.pending ? t('测试中…') : statusText(lanStatus)}</span>
          : null}
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
        <button type="button" onClick={() => { void copyDiagnostics(); }} style={smallButtonStyle}>{t('复制诊断信息')}</button>
        <button
          type="button"
          onClick={() => {
            setProxyStatus(null);
            setLanStatus(null);
            setCopyMessage(null);
            loadHealth();
            setTick((value) => value + 1);
          }}
          style={smallButtonStyle}
        >
          {t('重新检测')}
        </button>
        {copyMessage !== null
          ? <span style={hintStyle}>{copyMessage ? t('诊断信息已复制') : t('复制失败')}</span>
          : null}
      </div>
    </div>
  );
}

function WorkspaceHealthSection({ disabled }: { disabled: boolean }): React.ReactElement {
  const [results, setResults] = React.useState<WorkspaceHealthRow[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState('');

  const check = async () => {
    setLoading(true);
    setError('');
    try {
      setResults(await checkWorkspaceHealth([]));
    } catch (err) {
      setError(translateError(err instanceof Error ? err.message : err));
    } finally {
      setLoading(false);
    }
  };

  const reauth = async (result: WorkspaceHealthRow) => {
    let popup: Window | null = null;
    if (!isFnosHost() && !isFnosAppEmbedded()) popup = openAuthPopup();
    try {
      await authorizeWorkspacePath(result.path, popup);
      const [next] = await checkWorkspaceHealth([result.id]);
      if (next) {
        setResults((current) => current.map((item) => (item.id === next.id ? next : item)));
      }
      showToast(t('已重新授权'));
    } catch (err) {
      closePopup(popup);
      showToast(t('重新授权失败：{error}', {
        error: translateError(err instanceof Error ? err.message : err),
      }));
    }
  };

  const buttonStyle: React.CSSProperties = {
    ...smallButtonStyle,
    cursor: disabled || loading ? 'default' : 'pointer',
    opacity: disabled || loading ? 0.6 : 1,
  };
  const rowStyle: React.CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    padding: '8px 0',
    borderTop: '1px solid var(--dsw-alias-border-l3, rgba(127,127,127,.2))',
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10, paddingTop: 4 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
        <button
          type="button"
          onClick={() => { void check(); }}
          disabled={disabled || loading}
          style={buttonStyle}
        >
          {loading ? t('检查中…') : t('检查全部工作区')}
        </button>
        {results.length ? <span style={hintStyle}>{t('共 {count} 个', { count: results.length })}</span> : null}
        {error ? <span style={errorHintStyle}>{error}</span> : null}
        {(results.length || error) ? (
          <button
            type="button"
            onClick={() => {
              setResults([]);
              setError('');
            }}
            disabled={loading}
            style={buttonStyle}
          >
            {t('清除结果')}
          </button>
        ) : null}
      </div>
      {results.length ? (
        <div>
          {results.map((result) => {
            const healthy = result.exists && result.readable && result.writable;
            return (
              <div key={result.id} style={rowStyle}>
                <div style={{ minWidth: 0, flex: 1, display: 'flex', flexDirection: 'column', gap: 2 }}>
                  <span style={{
                    fontSize: 13,
                    fontWeight: 500,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}>{result.title || result.id}</span>
                  <span style={{
                    ...hintStyle,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }} title={result.path}>{result.path}</span>
                </div>
                <span style={{
                  flex: 'none',
                  fontSize: 12,
                  color: healthy
                    ? 'var(--dsw-alias-state-success-primary, #188038)'
                    : 'var(--dsw-alias-state-error-primary, #d93025)',
                }}>{workspaceHealthText(result)}</span>
                <button
                  type="button"
                  onClick={() => { void reauth(result); }}
                  disabled={disabled}
                  style={buttonStyle}
                >
                  {t('重新授权')}
                </button>
              </div>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
