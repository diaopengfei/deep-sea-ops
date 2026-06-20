import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Vite 配置: 开发时把 /api 请求代理到后端 8080 端口, 避免跨域
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
