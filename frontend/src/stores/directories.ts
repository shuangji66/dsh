// directories.ts —— 授权目录（含主目录切换）状态
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/serverapi'
import { useToastStore } from '@/stores/toast'

export const useDirectoriesStore = defineStore('directories', () => {
  const paths = ref<string[]>([])
  const convertedPaths = ref<Record<string, string>>({})
  const loading = ref(false)
  const uid = ref<number>(0)

  const toast = useToastStore()

  async function load() {
    loading.value = true
    try {
      const p = await api.fnosUserAccess(uid.value)
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
      const result = await api.convertPath(rawPaths, language)
      if (result?.result) {
        const map: Record<string, string> = {}
        result.result.forEach((item: { path: string; semanticPath: string }) => {
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
      await api.fnosDeleteUserAccess(uid.value, path)
      paths.value = paths.value.filter((p) => p !== path)
      delete convertedPaths.value[path]
    } catch (e) {
      toast.show((e as Error).message, 'error')
    }
  }

  return { paths, convertedPaths, loading, uid, load, remove }
})