<script setup lang="ts">
import { useToastStore } from '@/stores/toast'
import { computed } from 'vue'

const toast = useToastStore()

const typeClass = computed(() => {
  switch (toast.type) {
    case 'success': return 'bg-success'
    case 'error': return 'bg-danger'
    default: return 'bg-brand'
  }
})

const typeIcon = computed(() => {
  switch (toast.type) {
    case 'success':
      return '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="w-4 h-4"><polyline points="20 6 9 17 4 12"/></svg>'
    case 'error':
      return '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="w-4 h-4"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>'
    default:
      return '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="w-4 h-4"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>'
  }
})
</script>

<template>
  <Transition
    enter-active-class="transition duration-300 ease-out"
    enter-from-class="transform opacity-0 translate-y-4 scale-95"
    enter-to-class="transform opacity-100 translate-y-0 scale-100"
    leave-active-class="transition duration-200 ease-in"
    leave-from-class="transform opacity-100 translate-y-0 scale-100"
    leave-to-class="transform opacity-0 translate-y-4 scale-95"
  >
    <div
      v-if="toast.visible"
      class="fixed top-5 left-1/2 -translate-x-1/2 z-50 w-auto max-w-[90%] sm:max-w-sm shadow-card rounded-xl pointer-events-auto flex items-center p-3 sm:p-4 text-white"
      :class="typeClass"
      role="alert"
    >
      <span class="mr-3 flex-shrink-0 w-4 h-4 inline-block" v-html="typeIcon"></span>
      <span class="flex-1 text-sm font-medium break-words">{{ toast.message }}</span>
      <button
        @click="toast.hide"
        class="ml-3 flex-shrink-0 text-white/80 hover:text-white focus:outline-none"
      >
        <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 sm:h-5 sm:w-5" viewBox="0 0 20 20" fill="currentColor">
          <path fill-rule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clip-rule="evenodd" />
        </svg>
      </button>
    </div>
  </Transition>
</template>