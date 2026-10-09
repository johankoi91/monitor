import { defineConfig } from 'vite';
export default defineConfig({
  build: {outDir: '../web/static', emptyOutDir: true},
  server: {proxy: {'/api': 'http://127.0.0.1:18086', '/health': 'http://127.0.0.1:18086'}}
});
