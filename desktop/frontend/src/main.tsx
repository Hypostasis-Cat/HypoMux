import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";

const isEditableTarget = (target: EventTarget | null) =>
  target instanceof Element &&
  Boolean(target.closest("input, textarea, [contenteditable='true']"));

document.addEventListener("contextmenu", (event) => {
  if (!isEditableTarget(event.target)) {
    event.preventDefault();
  }
});

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
