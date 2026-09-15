import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";

// Suppress the browser menu in inputs too. Preventing the default action
// preserves keyboard editing shortcuts and app-specific context menu handlers.
document.addEventListener("contextmenu", (event) => {
  event.preventDefault();
});

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
