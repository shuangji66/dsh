/**
 * Recognition helpers for dsh's composer command menu (the "+" menu).
 *
 * The menu is identified by the English command aliases it renders next to the
 * localized labels ("文件 file", "目标 goal", "计划 plan", …). Aliases come
 * from the command names, so they survive UI language changes; CSS-module
 * class names and group titles do not.
 */

/**
 * Whether a list of option texts looks like the composer command menu.
 *
 * @param {string[]} labels
 * @returns {boolean}
 */
export function looksLikeComposerCommandMenu(labels) {
  const text = labels.map((value) => String(value == null ? '' : value).toLowerCase());
  if (text.length < 3) return false;
  return text.some((item) => /\bfile\b/.test(item))
    && text.some((item) => /\bgoal\b/.test(item))
    && text.some((item) => /\bplan\b/.test(item));
}
