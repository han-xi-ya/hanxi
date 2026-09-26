import { cpSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import wails from "@wailsio/runtime/plugins/vite";

// N36 分发义务：src/assets/fonts/licenses/ 下的 OFL 许可文本未被任何模块引用，
// vite 不会自动打包；构建结束时显式拷入 dist/font-licenses/，随最终 EXE 分发
// （双保险另一路：AboutView 字体许可全文折叠段，见 src/views/AboutView.vue）。
let licenseOutDir = fileURLToPath(new URL("./dist", import.meta.url));
const fontLicenseCopy = {
  name: "font-license-copy",
  apply: "build" as const,
  configResolved(config: { root: string; build: { outDir: string } }) {
    licenseOutDir = resolve(config.root, config.build.outDir);
  },
  closeBundle() {
    cpSync(
      fileURLToPath(new URL("./src/assets/fonts/licenses", import.meta.url)),
      resolve(licenseOutDir, "font-licenses"),
      { recursive: true },
    );
  },
};

// https://vitejs.dev/config/
export default defineConfig({
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  plugins: [vue(), wails("./bindings"), fontLicenseCopy],
});
