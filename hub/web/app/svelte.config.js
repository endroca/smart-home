import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";

export default {
  preprocess: vitePreprocess(),
  compilerOptions: {
    // Runes em todo o projeto (Svelte 5): estado explicito, sem `export let`.
    runes: true,
  },
};
