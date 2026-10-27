/**
 * Injected stylesheet for the plugin's own DOM nodes.
 *
 * The plugin cannot import dsh's CSS modules (they are build-time hashes), so
 * its injected nodes are styled here with the theme's `--dsw-alias-*` custom
 * properties and neutral fallbacks. Class names are prefixed `dsh-fnos-*` to
 * make the uninstall path (and debugging in a device console) unambiguous.
 */
import { STYLE_ID } from './constants.ts';

const STYLE_TEXT = `
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

/** Inject the stylesheet once per page. */
export function injectStyle(): void {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = STYLE_TEXT;
  (document.head || document.documentElement).appendChild(style);
}
