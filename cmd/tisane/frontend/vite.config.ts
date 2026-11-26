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

function resolveDictionaryCredits(): string {
  const fromEnv = process.env.TISANE_DICTIONARY_CREDITS_FILE?.trim();
  const creditsPath =
    fromEnv || fileURLToPath(new URL('../../../dictionaries/CREDITS.md', import.meta.url));
  return readFileSync(creditsPath, 'utf8');
}

export default defineConfig({
  plugins: [svelte(), svelteTesting(), tailwindcss()],
  define: {
    __APP_VERSION__: JSON.stringify(resolveAppVersion()),
    __DICTIONARY_CREDITS__: JSON.stringify(resolveDictionaryCredits())
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
