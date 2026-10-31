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
  // 防崩护栏，别当死代码删：xterm 6.0.0 出厂即预压缩 ESM，Vite 生产构建的 esbuild
  // 再压缩一轮会毁 requestMode 闭包（xtermjs/xterm.js#5800），此行为防崩护栏，
  // 别当死代码删。⚠️ 现状实测备案（2026-10-31）：Vite 8.3.4（rolldown+oxc 管线）
  // 已不带 esbuild（--minify esbuild 直接报"requires esbuild to be installed
  // separately"），本行在 Vite 8 下不产生压缩效果——现管 minifier 是 oxc，它把
  // 已压缩产物重新 parse 成 AST 再改名，#5800 那类"预压缩文本二次压缩"的病根
  // 在 AST 级重命名下不存在；终端真机回归（vim/DCS 生产构建）是第一道真闸门。
  // 若工具链回退 Vite ≤7（esbuild 管线）本行立即恢复效力，故保留不删。
  esbuild: { minifyIdentifiers: false },
  plugins: [vue(), wails("./bindings"), fontLicenseCopy],
});
