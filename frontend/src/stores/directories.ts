// directories.ts —— 授权目录（含主目录切换）状态
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/serverapi'
import { useToastStore } from '@/stores/toast'
import { useI18n } from '@/composables/useI18n'

// fnOS 侧的业务失败是 **HTTP 200 + {ok:false, error|msg}**（见 backend/admin.go 的 handleFnos），
// 只有非 2xx 才会被 serverapi 的 request() 抛出。不判 ok === false 就会把失败当成功：
// 拉取失败显示成「暂无授权目录」、删除失败卡片照样消失。
// serverapi 的返回类型里个别分支没声明 error 字段（不改那个文件），这里用局部类型断言取用。
type FnosResult = { ok?: boolean; paths?: string[]; msg?: string; error?: string }

export const useDirectoriesStore = defineStore('directories', () => {
  const paths = ref<string[]>([])
  const convertedPaths = ref<Record<string, string>>({})
  const loading = ref(false)
  const uid = ref<number>(0)

  const toast = useToastStore()
  const { t } = useI18n()

  async function load() {
    loading.value = true
    try {
      const p = (await api.fnosUserAccess(uid.value)) as unknown as FnosResult
      if (p.ok === false) {
        // 失败时**不动**本地 paths：否则页面会从「有目录」变成「暂无授权目录」，误导用户
        toast.show(p.error || p.msg || t('directory_load_failed'), 'error')
        return
      }
      paths.value = p.paths || []
      if (paths.value.length > 0) {
        await convertPaths(paths.value)
      }
    } catch (e) {
      toast.show((e as Error).message, 'error')
    } finally {
      loading.value = false
    }
  }

  async function convertPaths(rawPaths: string[]) {
    try {
      const language = navigator.language || 'zh-CN'
      const result = (await api.convertPath(rawPaths, language)) as unknown as {
        ok?: boolean
        result?: Array<{ path: string; semanticPath: string }>
        error?: string
        msg?: string
      }
      if (result?.ok === false) {
        // 转换失败不影响路径列表本身，保留既有映射（不要清空成「没有语义路径」）
        toast.show(result.error || result.msg || t('directory_convert_failed'), 'error')
        return
      }
      if (result?.result) {
        const map: Record<string, string> = {}
        result.result.forEach((item) => {
          map[item.path] = item.semanticPath
        })
        convertedPaths.value = map
      }
    } catch (e) {
      console.warn('路径转换失败:', e)
    }
  }

  async function remove(path: string) {
    try {
      const r = (await api.fnosDeleteUserAccess(uid.value, path)) as unknown as FnosResult
      if (r.ok === false) {
        // 删除失败时保留卡片（本地列表不动），并说明原因
        toast.show(r.error || r.msg || t('directory_remove_failed'), 'error')
        return
      }
      paths.value = paths.value.filter((p) => p !== path)
      delete convertedPaths.value[path]
    } catch (e) {
      toast.show((e as Error).message, 'error')
    }
  }

  return { paths, convertedPaths, loading, uid, load, remove }
})
