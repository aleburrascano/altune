import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { App } from "./App";
import { routerBasename } from "./routes";
import "./styles.css";

// The router's basename is Vite's mount prefix ("/overseer/" in prod, "/" in dev),
// so client-side routes resolve inside the Caddy mount without hard-coding it.
const basename = routerBasename(import.meta.env.BASE_URL);

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <BrowserRouter basename={basename}>
        <App />
      </BrowserRouter>
    </StrictMode>,
  );
}
