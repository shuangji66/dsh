// 轻量 Markdown 渲染（基于 marked）。
//
// 用于把来自外部来源的 Markdown 文本（如 GitHub release 正文）安全地渲染成
// HTML。因为渲染结果会经 v-html 注入页面，这里对不可信内容做了两层防御：
//   1. 源文本中的原始 HTML（<script> 等）一律转义为文本，不进入 DOM；
//   2. 链接只放行 http/https/mailto 协议，其余协议（如 javascript:）降级为
//      纯文本；图片不放行 <img>，改为可点击的文字链接，避免引入外部加载
//      与隐私泄露。
//
// 开启 GFM 与 breaks：单换行按 <br> 展示，与现有“保留换行”的日志观感一致。

import { Marked } from 'marked'

// 转义 HTML 特殊字符，作为一切外部文本进入 HTML 的兜底。
function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

// 只放行常见的文本传输协议；其余（javascript:、data: 等）一律拒绝。
const SAFE_PROTOCOL_RE = /^(https?:|mailto:)/i

const markdown = new Marked(
  { gfm: true, breaks: true },
  {
    renderer: {
      // 原始 HTML 一律转义为可见文本（绝不透传标签）。
      html(token) {
        return escapeHtml(token.text || '')
      },
      // 链接：仅当 href 是安全协议时才生成 <a>，否则只输出链接文字。
      link(token) {
        const href = (token.href || '').trim()
        const label = this.parser ? this.parser.parseInline(token.tokens || []) : escapeHtml(token.text || '')
        if (!SAFE_PROTOCOL_RE.test(href)) {
          return label
        }
        const title = token.title ? ` title="${escapeHtml(token.title)}"` : ''
        return `<a href="${escapeHtml(href)}"${title} target="_blank" rel="noopener noreferrer">${label}</a>`
      },
      // 图片：不放行 <img>，转成指向图片地址的文字链接（保留 alt 文本）。
      image(token) {
        const alt = escapeHtml(token.text || token.href || '')
        const href = (token.href || '').trim()
        if (SAFE_PROTOCOL_RE.test(href)) {
          return `<a href="${escapeHtml(href)}" target="_blank" rel="noopener noreferrer">${alt}</a>`
        }
        return alt
      }
    }
  }
)

// 把 Markdown 源文本渲染成安全的 HTML 字符串（供 v-html 使用）。
export function renderMarkdown(source: string): string {
  if (!source) return ''
  return markdown.parse(source, { async: false })
}