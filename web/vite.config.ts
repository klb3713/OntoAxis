import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

/**
 * Vite 开发与构建配置。
 *
 * - 开发服务器把 `/api` 代理到本地 `ontoaxis-core`（默认 `http://127.0.0.1:8080`）。
 * - `VITE_DEV_PROXY_TARGET` 可覆盖代理目标，不写入任何真实凭据。
 * - 生产构建产物输出到 `web/dist`，由 oa-edge/Caddy 托管并同源转发 `/api/v1`。
 */
export default defineConfig(({ mode }) => {
  const proxyTarget = process.env.VITE_DEV_PROXY_TARGET ?? "http://127.0.0.1:8080";

  return {
    plugins: [react()],
    server: {
      port: 5173,
      strictPort: true,
      proxy: {
        "/api": {
          target: proxyTarget,
          changeOrigin: true,
        },
      },
    },
    preview: {
      port: 4173,
      strictPort: true,
    },
    build: {
      outDir: "dist",
      sourcemap: mode !== "production",
      target: "es2022",
    },
  };
});
