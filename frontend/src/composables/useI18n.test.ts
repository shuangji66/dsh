import { beforeEach, describe, expect, it } from 'vitest'
import { setLocale, uiErrText, uiText, useI18n } from '@/composables/useI18n'

// 只测「前端自己可控」的部分：界面文案、参数替换、后端 code 的回退。
// 后端 uimsg_test.go 负责保证 code 在 zh/en 两份字典里都存在。
describe('i18n', () => {
  beforeEach(() => {
    localStorage.clear()
    setLocale('zh')
  })

  it('translates UI keys and switches locale', () => {
    const { t } = useI18n()
    expect(t('nav_overview')).toBe('概览')
    setLocale('en')
    expect(t('nav_overview')).toBe('Overview')
  })

  it('localizes mirror slugs inside backend-provided params', () => {
    const ref = {
      code: 'msg_install_from_mirror',
      params: { mirror: 'aliyun', pkg: 'dsh', version: '0.2.2' }
    }
    expect(uiText('', ref)).toBe('正在从 阿里云 下载 dsh@0.2.2')
    setLocale('en')
    expect(uiText('', ref)).toBe('Downloading dsh@0.2.2 from Aliyun')
  })

  it('falls back to the backend original text when code is unknown', () => {
    expect(uiText('后端原文', { code: 'err_not_in_frontend' })).toBe('后端原文')
    expect(uiText('', undefined, 'fallback')).toBe('fallback')
  })

  it('renders ApiError-like objects through uiErrText', () => {
    expect(uiErrText({ code: 'err_need_version', params: {} }, 'fallback')).toBe('缺少 version')
    expect(uiErrText(new Error('boom'), 'fallback')).toBe('boom')
  })
})
