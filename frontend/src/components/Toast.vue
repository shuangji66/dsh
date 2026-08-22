<script setup lang="ts">
import { useToastStore } from '@/stores/toast'
import { computed } from 'vue'

const toast = useToastStore()

const typeClass = computed(() => {
  switch (toast.type) {
    case 'success': return 'bg-emerald-500 dark:bg-emerald-600'
    case 'error': return 'bg-rose-500 dark:bg-rose-600'
    default: return 'bg-blue-500 dark:bg-blue-600'
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
      class="fixed top-4 left-1/2 -translate-x-1/2 z-50 w-auto max-w-[90%] sm:max-w-sm shadow-lg rounded-lg pointer-events-auto flex items-center p-3 sm:p-4 text-white"
      :class="typeClass"
      role="alert"
    >
      <span class="flex-1 text-sm font-medium break-words">{{ toast.message }}</span>
      <button
        @click="toast.hide"
        class="ml-3 flex-shrink-0 text-white hover:text-gray-200 focus:outline-none"
      >
        <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 sm:h-5 sm:w-5" viewBox="0 0 20 20" fill="currentColor">
          <path fill-rule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clip-rule="evenodd" />
        </svg>
      </button>
    </div>
  </Transition>
</template>