<script setup lang="ts">
import { onMounted } from 'vue'
import { storeToRefs } from 'pinia'
import { useSettingsStore } from '@/stores/settings'

const store = useSettingsStore()
const { config, status, locked, loading } = storeToRefs(store)

onMounted(() => store.load())
</script>

<template>
  <div class="p-8 max-w-3xl">
    <p class="text-slate-500 dark:text-slate-400 text-sm mb-8">DeepSeek Harness 控制台</p>

    <!-- dsh 生命周期 -->
    <section class="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-xl p-6 mb-6">
      <div class="flex items-center justify-between mb-4">
        <h2 class="font-semibold text-slate-900 dark:text-slate-100">DeepSeek Harness</h2>
        <span class="text-sm" :class="status?.running ? 'text-emerald-600 dark:text-emerald-400' : 'text-slate-500 dark:text-slate-400'">
          <span class="inline-block w-2 h-2 rounded-full mr-1 align-middle" :class="status?.running ? 'bg-emerald-600 dark:bg-emerald-400' : 'bg-slate-500 dark:bg-slate-400'"></span>
        {{ status?.running ? '运行中' : '已停止' }}
        </span>
      </div>
      <div class="flex gap-3">
        <button
          v-if="!status?.running"
          class="px-4 py-2 bg-emerald-600 hover:bg-emerald-500 rounded-lg text-sm font-medium disabled:opacity-50 text-white"
          :disabled="loading"
          @click="store.startDsh()"
        >启动</button>
        <button
          v-else
          class="px-4 py-2 bg-rose-600 hover:bg-rose-500 rounded-lg text-sm font-medium disabled:opacity-50 text-white"
          :disabled="loading"
          @click="store.stopDsh()"
        >停止</button>
        <button
          class="px-4 py-2 bg-blue-600 hover:bg-blue-500 rounded-lg text-sm font-medium disabled:opacity-50 text-white"
          :disabled="loading"
          @click="store.restartDsh()"
        >重启</button>
      </div>
    </section>

    <!-- 反向代理与鉴权 -->
    <section class="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-xl p-6 mb-6">
      <h2 class="font-semibold mb-4 text-slate-900 dark:text-slate-100">设置</h2>
      <div class="space-y-4">
        <!-- 启用外部代理（开关） -->
        <label class="flex items-center justify-between cursor-pointer">
          <span class="text-sm text-slate-700 dark:text-slate-300">启用代理</span>
          <div class="relative inline-flex items-center">
            <input type="checkbox" v-model="config.proxyEnabled" class="sr-only peer">
            <div class="w-11 h-6 bg-slate-300 dark:bg-slate-600 rounded-full peer peer-checked:bg-indigo-600 transition-colors"></div>
            <div class="absolute left-0.5 top-0.5 w-5 h-5 bg-white rounded-full transition-transform peer-checked:translate-x-5"></div>
          </div>
        </label>

        <div v-if="config.proxyEnabled">
          <label class="block text-sm text-slate-600 dark:text-slate-400 mb-1">代理地址</label>
          <input v-model="config.proxyAddr" class="w-full px-3 py-2 bg-slate-100 dark:bg-slate-700 border border-slate-300 dark:border-slate-600 rounded-lg text-sm outline-none focus:border-indigo-500 text-slate-900 dark:text-slate-100" />
        </div>

        <div class="grid grid-cols-2 gap-4">
          <div>
            <label class="block text-sm text-slate-600 dark:text-slate-400 mb-1">dsh 端口</label>
            <input
              v-model.number="config.dshPort"
              type="number"
              min="1"
              max="65535"
              :disabled="locked"
              class="w-full px-3 py-2 bg-slate-100 dark:bg-slate-700 border border-slate-300 dark:border-slate-600 rounded-lg text-sm outline-none focus:border-indigo-500 disabled:opacity-50 disabled:cursor-not-allowed text-slate-900 dark:text-slate-100"
            />
          </div>
        </div>

        <!-- 启用登录鉴权（开关） -->
        <label class="flex items-center justify-between cursor-pointer">
          <span class="text-sm text-slate-700 dark:text-slate-300">启用登录鉴权</span>
          <div class="relative inline-flex items-center">
            <input type="checkbox" v-model="config.authEnabled" class="sr-only peer">
            <div class="w-11 h-6 bg-slate-300 dark:bg-slate-600 rounded-full peer peer-checked:bg-indigo-600 transition-colors"></div>
            <div class="absolute left-0.5 top-0.5 w-5 h-5 bg-white rounded-full transition-transform peer-checked:translate-x-5"></div>
          </div>
        </label>

        <div>
          <label class="block text-sm text-slate-600 dark:text-slate-400 mb-1">
            访问密码 <span class="text-slate-500 dark:text-slate-500">(≥8位，含字母/数字/标点)</span>
          </label>
          <input v-model="config.password" type="password" placeholder="留空则鉴权不生效"
            class="w-full px-3 py-2 bg-slate-100 dark:bg-slate-700 border border-slate-300 dark:border-slate-600 rounded-lg text-sm outline-none focus:border-indigo-500 text-slate-900 dark:text-slate-100" />
        </div>
      </div>
      <button class="mt-6 px-4 py-2 bg-indigo-600 hover:bg-indigo-500 rounded-lg text-sm font-medium disabled:opacity-50 text-white"
        :disabled="loading" @click="store.save()">保存配置</button>
    </section>
  </div>
</template>