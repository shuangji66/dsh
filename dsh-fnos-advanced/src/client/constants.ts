/**
 * Shared constants and DOM anchors for the browser half.
 *
 * The plugin works by observation, not by importing dsh's UI packages, so every
 * selector here is an intentional contract with dsh's rendered markup (or an
 * attribute the plugin itself adds). Keep the `data-*` names stable: styles,
 * scanners and the uninstall path all look them up.
 */

export const LOG_PREFIX = '[dsh-fnos-advanced]';

/** postMessage envelope id used by the authorization callback page. */
export const MSG_TYPE = 'dsh-fnos-advanced:auth';
/** localStorage key the callback page writes its result to. */
export const STORAGE_KEY = 'dsh-fnos-advanced:auth-result';
/** localStorage key for a browser-side fnOS base URL override. */
export const AUTH_BASE_KEY = 'dsh-fnos-advanced:auth-base';
/** Slot key of the plugin-center settings page. */
export const SETTINGS_SLOT_KEY = 'dsh-fnos-advanced';

/** Marker on the "select fnOS folder" button (and its uninstall cleanup). */
export const BUTTON_ATTR = 'data-dsh-fnos-advanced';
/** Marker on the injected composer command row. */
export const PICK_ITEM_ATTR = 'data-dsh-fnos-pick-file';
/** Marker on the injected file row in dsh's "@" completion menu. */
export const REF_PICK_ITEM_ATTR = 'data-dsh-fnos-ref-pick';
/** Marker on the per-row file-tree action button. */
export const FILE_ACTION_ATTR = 'data-dsh-fnos-file-action';
/** Marker on an action button rendered through a dsh slot (React owns that node). */
export const SLOT_ACTION_ATTR = 'data-dsh-fnos-slot-action';
/** Marker on the injected workspace-menu rows. */
export const OPEN_DIR_ATTR = 'data-dsh-fnos-open-dir';
export const CHECK_DIR_ATTR = 'data-dsh-fnos-check-dir';
export const REAUTH_DIR_ATTR = 'data-dsh-fnos-reauth-dir';

export const STATUS_CLASS = 'dsh-fnos-advanced-status';
export const STYLE_ID = 'dsh-fnos-advanced-style';

/** Popup window features; sized for fnOS's authorization page. */
export const POPUP_FEATURES = 'popup=yes,width=760,height=680,resizable=yes,scrollbars=yes';
/** How long a web authorization may take before the flow gives up. */
export const AUTH_TIMEOUT_MS = 5 * 60 * 1000;

// Text probes. The picker is a React dialog owned by dsh, so its identity is
// derived from accessible labels (localized) plus structural fallbacks.
export const WORKSPACE_TITLE_RE = /workspace|工作区/i;
export const PATH_LABEL_RE = /(?:^|\b)(?:edit\s*path|path)(?:\b|$)|路径|编辑路径|修改路径/i;
export const OPEN_BUTTON_RE = /^(?:open|打开|确定|选择|select|choose)/i;
export const CANCEL_BUTTON_RE = /^(?:cancel|取消)/i;

/** Folder icon used by the workspace menu rows. */
export const FOLDER_OPEN_SVG = '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" stroke-width="1"><path d="M1.5 3.5C1.5 2.95 1.95 2.5 2.5 2.5H5.4C5.65 2.5 5.88 2.59 6.07 2.75L7.7 4.2C7.88 4.36 8.12 4.45 8.36 4.45H13.5C14.05 4.45 14.5 4.9 14.5 5.45V12.5C14.5 13.05 14.05 13.5 13.5 13.5H2.5C1.95 13.5 1.5 13.05 1.5 12.5V3.5Z" stroke="currentColor"/></svg>';
/** File-with-plus icon used by the composer command row. */
export const PICK_FILE_SVG = '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path d="M9.5 1.5H4.5C3.95 1.5 3.5 1.95 3.5 2.5V13.5C3.5 14.05 3.95 14.5 4.5 14.5H11.5C12.05 14.5 12.5 14.05 12.5 13.5V4.5L9.5 1.5Z" stroke="currentColor" stroke-linejoin="round"/><path d="M9.5 1.5V4.5H12.5" stroke="currentColor" stroke-linejoin="round"/><path d="M6 8.5H10M8 6.5V10.5" stroke="currentColor" stroke-linecap="round"/></svg>';
/** "…" icon used by the file-tree row action. */
export const FILE_ACTION_SVG = '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><circle cx="3.5" cy="8" r="1.15" fill="currentColor"/><circle cx="8" cy="8" r="1.15" fill="currentColor"/><circle cx="12.5" cy="8" r="1.15" fill="currentColor"/></svg>';
