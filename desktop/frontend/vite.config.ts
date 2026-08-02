import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";
import { resolve } from "path";

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  plugins: [react(), wails("./bindings")],
  build: {
    rollupOptions: {
      input: {
        main: resolve(__dirname, "index.html"),
        trayMenu: resolve(__dirname, "tray-menu.html"),
      },
    },
  },
});
