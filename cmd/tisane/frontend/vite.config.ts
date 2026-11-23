import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { svelteTesting } from '@testing-library/svelte/vite';
import tailwindcss from '@tailwindcss/vite';

function resolveAppVersion(): string {
  const fromEnv = process.env.TISANE_VERSION?.trim();
  if (fromEnv) {
    return fromEnv;
  }
  const versionPath = fileURLToPath(new URL('../../../VERSION', import.meta.url));
  return readFileSync(versionPath, 'utf8').trim();
}

export default defineConfig({
  plugins: [svelte(), svelteTesting(), tailwindcss()],
  define: {
    __APP_VERSION__: JSON.stringify(resolveAppVersion())
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test-setup.ts']
  }
});
