// Depends on @example/math (turbo builds that first via ^build). Sleeps ~3s to
// simulate bundling, then writes its own artifact into dist/.
const fs = require("node:fs");
const start = Date.now();
while (Date.now() - start < 3000) {} // simulate CPU work
fs.mkdirSync("dist", { recursive: true });
fs.writeFileSync("dist/bundle.js", "console.log('hello from @example/app');\n");
console.log("[@example/app] built dist/bundle.js in ~3s");
