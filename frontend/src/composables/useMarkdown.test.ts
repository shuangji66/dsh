import { describe, expect, it } from 'vitest'
import { renderMarkdown } from '@/composables/useMarkdown'

// 外部 Markdown（GitHub release 正文）会经 v-html 注入，安全性由这里兜底。
describe('renderMarkdown', () => {
  it('escapes raw HTML instead of injecting it', () => {
    const html = renderMarkdown('<script>alert(1)</script>')
    expect(html).not.toContain('<script>')
    expect(html).toContain('&lt;script&gt;')
  })

  it('only allows http/https/mailto links', () => {
    const js = renderMarkdown('[click](javascript:alert(1))')
    expect(js).not.toContain('href="javascript:')
    expect(js).toContain('click')

    const https = renderMarkdown('[repo](https://github.com/deepseek-ai/deepseek-harness)')
    expect(https).toContain('href="https://github.com/deepseek-ai/deepseek-harness"')
    expect(https).toContain('rel="noopener noreferrer"')
  })

  it('turns images into links (no external image loads)', () => {
    const html = renderMarkdown('![alt](https://example.com/a.png)')
    expect(html).not.toContain('<img')
    expect(html).toContain('href="https://example.com/a.png"')
    expect(html).toContain('alt')
  })

  it('renders GFM basics', () => {
    expect(renderMarkdown('**bold**')).toContain('<strong>bold</strong>')
    expect(renderMarkdown('- a\n- b')).toContain('<li>a</li>')
    expect(renderMarkdown('')).toBe('')
  })

  // 插件详情：README 的徽章 HTML 块在控制台无法渲染，转义后的标签源码没人看 —— 整块藏掉。
  it('hides raw HTML blocks when dropHtml is set', () => {
    const source = [
      '<p align="center"> <img src="https://img.shields.io/npm/v/x" alt="Version"> &nbsp; </p>',
      '',
      '# 正文',
      '',
      '这是说明。'
    ].join('\n')
    const html = renderMarkdown(source, { dropHtml: true })
    expect(html).not.toContain('&lt;p')
    expect(html).not.toContain('img.shields.io')
    expect(html).toContain('<h1>正文</h1>')
    expect(html).toContain('这是说明。')
  })

  it('drops inline HTML tags when dropHtml is set', () => {
    const html = renderMarkdown('前文<br>后文', { dropHtml: true })
    expect(html).not.toContain('&lt;br')
    expect(html).not.toContain('<br')
    expect(html).toContain('前文')
    expect(html).toContain('后文')
  })
})
