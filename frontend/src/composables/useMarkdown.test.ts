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
})
