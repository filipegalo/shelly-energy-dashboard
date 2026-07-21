import { copyFile } from "node:fs/promises";

await copyFile("node_modules/htmx.org/dist/htmx.min.js", "internal/web/static/js/htmx.min.js");
await copyFile("node_modules/chart.js/dist/chart.umd.js", "internal/web/static/js/chart.umd.js");
