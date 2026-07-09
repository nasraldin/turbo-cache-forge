// Pretend this is a real compile step. It sleeps ~2s so the difference between
// a cold build and a cache HIT is obvious, then emits a build artifact into dist/.
const fs = require("node:fs");
const start = Date.now();
while (Date.now() - start < 2000) {} // simulate CPU work
fs.mkdirSync("dist", { recursive: true });
fs.writeFileSync("dist/index.js", "exports.add = (a, b) => a + b;\n");
console.log("[@example/math] built dist/index.js in ~2s");
