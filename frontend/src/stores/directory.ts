// directory.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/serverapi'
import { TrimApp } from '@trimjs/web-app'
import { useToastStore } from '@/stores/toast'

export const useDirectoryStore = defineStore('directory', () => {
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
      const sdk = new TrimApp()
      const language = navigator.language || 'zh-CN'
      const result = await sdk.convertPath({
        path: rawPaths,
        language: language
      })
      if (result?.data?.result) {
        const map: Record<string, string> = {}
        result.data.result.forEach((item: { path: string; semanticPath: string }) => {
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