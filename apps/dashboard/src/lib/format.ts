export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  // Binary (1024-based) scaling — label as KiB/MiB/GiB per design brief, not decimal KB/MB/GB.
  const units = ["KiB", "MiB", "GiB", "TiB", "PiB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  const s = v % 1 === 0 ? v.toFixed(0) : v.toFixed(1);
  return `${s} ${units[i]}`;
}

export function formatPercent(ratio: number): string {
  const pct = ratio * 100;
  return `${pct % 1 === 0 ? pct.toFixed(0) : pct.toFixed(1)}%`;
}
