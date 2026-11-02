import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  base: './',
  plugins: [vue(), tailwindcss()],
  server: { fs: { allow: [fileURLToPath(new URL('.', import.meta.url)), fileURLToPath(new URL('../../locale', import.meta.url))] } },
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
})
