import { defineStore } from 'pinia'
import { ref } from 'vue'

export type ToastType = 'info' | 'success' | 'error'

export const useToastStore = defineStore('toast', () => {
  const visible = ref(false)
  const message = ref('')
  const type = ref<ToastType>('info')
  let timer: ReturnType<typeof setTimeout> | null = null

  function show(msg: string, t: ToastType = 'info', duration = 3000) {
    if (timer) clearTimeout(timer)
    message.value = msg
    type.value = t
    visible.value = true
    timer = setTimeout(() => {
      visible.value = false
      timer = null
    }, duration)
  }

  function hide() {
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
    visible.value = false
  }

  return { visible, message, type, show, hide }
})