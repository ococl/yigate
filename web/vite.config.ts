import { fileURLToPath, URL } from 'node:url'
import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import AutoImport from 'unplugin-auto-import/vite'
import { AntdvNextResolver } from '@antdv-next/auto-import-resolver'

export default defineConfig(({ mode }) => {
  // 开发服务器 API 代理目标，可在 .env.development.local 中通过 VITE_API_TARGET 覆盖
  const env = loadEnv(mode, process.cwd(), '')
  const apiTarget = env.VITE_API_TARGET || 'http://127.0.0.1:8890'

  return {
    plugins: [
      vue(),
      AutoImport({
        imports: ['vue', 'vue-router', 'pinia'],
        dts: 'src/auto-imports.d.ts',
      }),
      Components({
        resolvers: [AntdvNextResolver()],
        dts: 'src/components.d.ts',
      }),
    ],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    build: {
      rollupOptions: {
        output: {
          // 仅拆分框架内核，组件库继续按路由/组件自然分块，避免首屏加载整包
          manualChunks(id) {
            if (/[\\/]node_modules[\\/](@vue|vue|vue-router|pinia)[\\/]/.test(id)) {
              return 'vendor-vue'
            }
          },
        },
      },
    },
    server: {
      port: 5173,
      proxy: {
        '/api': {
          target: apiTarget,
          changeOrigin: true,
        },
        '/health': {
          target: apiTarget,
          changeOrigin: true,
        },
        '/metrics': {
          target: apiTarget,
          changeOrigin: true,
        },
        '/debug': {
          target: apiTarget,
          changeOrigin: true,
        },
      },
    },
  }
})
