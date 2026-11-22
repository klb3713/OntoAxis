import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { createAppRuntime } from "./app/runtime";
import "./styles.css";

const container = document.getElementById("root");

if (!container) {
  throw new Error("缺少 #root 挂载点");
}

try {
  const runtime = createAppRuntime();
  createRoot(container).render(
    <StrictMode>
      <App runtime={runtime} />
    </StrictMode>,
  );
} catch (error) {
  const message = error instanceof Error ? error.message : String(error);
  container.innerHTML = `<div style="padding:32px;font-family:sans-serif"><h1>OntoAxis 无法启动</h1><p>${message}</p></div>`;
}
