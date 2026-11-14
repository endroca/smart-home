/// <reference types="vitest/config" />
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";
import { VitePWA } from "vite-plugin-pwa";

// O hub embute a UI compilada (../dist) no binario Go. O build sai daqui
// (hub/web/app) para hub/web/dist, com base "/" porque o Go serve na raiz.
export default defineConfig({
  base: "/",
  plugins: [
    svelte(),
    tailwindcss(),
    VitePWA({
      registerType: "autoUpdate",
      injectRegister: "auto",
      // O manifest e proprio (pt-BR), entao desligamos o gerado.
      manifest: false,
      includeAssets: [
        "assets/favicon.ico",
        "assets/apple-touch-icon.png",
        "assets/icon-192.png",
        "assets/icon-512.png",
        "assets/icon-maskable-512.png",
      ],
      workbox: {
        globPatterns: ["**/*.{js,css,html,svg,png,ico,webmanifest}"],
        navigateFallback: "/index.html",
        // Estado ao vivo e downloads de firmware nunca entram no cache.
        navigateFallbackDenylist: [/^\/api\//, /^\/firmware\//],
        runtimeCaching: [],
      },
      devOptions: { enabled: false },
    }),
  ],
  build: {
    outDir: "../dist",
    emptyOutDir: true,
    assetsDir: "assets",
    target: "es2020",
  },
  // No vitest, resolve o build de navegador do Svelte (e nao o de servidor).
  resolve: {
    alias: {
      $lib: fileURLToPath(new URL("./src/lib", import.meta.url)),
    },
    conditions: process.env.VITEST ? ["browser"] : undefined,
  },
  server: {
    host: true,
    port: 5173,
    // HMR contra o hub em :8080: a UI fala com a API real da LAN.
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: true, ws: true },
      "/firmware": { target: "http://localhost:8080", changeOrigin: true },
    },
  },
  test: {
    globals: true,
    environment: "happy-dom",
    setupFiles: ["./vitest-setup.ts"],
    include: ["src/**/*.{test,spec}.ts"],
    css: false,
  },
});
