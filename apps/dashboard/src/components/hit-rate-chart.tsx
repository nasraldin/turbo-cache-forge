"use client";
import ReactECharts from "echarts-for-react";
import type { StatsPoint } from "@tcf/types";

// The one ECharts trend on the whole dashboard (Cache Statistics page) — per
// dataviz discipline: two labeled series (Hits/Misses), a legend, axis
// labels, an axis tooltip, no gradient chartjunk. Colors are the design
// brief's HIT/MISS palette (teal/amber), never generic chart blue. Axis/text
// colors read the theme's CSS variables at render so the chart stays legible
// in both light and dark themes; the two series hexes stay literal since
// HIT/MISS is the same in both themes per the brief.
export function HitRateChart({ points }: { points: StatsPoint[] }) {
  const css = (v: string, fb: string) =>
    typeof window === "undefined"
      ? fb
      : getComputedStyle(document.documentElement).getPropertyValue(v).trim() || fb;
  const muted = css("--muted", "#8b98a9");
  const border = css("--border", "#232a34");
  const option = {
    tooltip: { trigger: "axis" },
    legend: { data: ["Hits", "Misses"], bottom: 0, textStyle: { color: muted } },
    grid: { left: 44, right: 16, top: 16, bottom: 40 },
    xAxis: {
      type: "category",
      data: points.map((p) => p.day),
      axisLabel: { color: muted },
      axisLine: { lineStyle: { color: border } },
    },
    yAxis: {
      type: "value",
      axisLabel: { color: muted },
      splitLine: { lineStyle: { color: border } },
    },
    series: [
      {
        name: "Hits",
        type: "line",
        smooth: true,
        areaStyle: { opacity: 0.12 },
        data: points.map((p) => p.hits),
        color: "#3FB98C",
      },
      {
        name: "Misses",
        type: "line",
        smooth: true,
        data: points.map((p) => p.misses),
        color: "#E0863A",
      },
    ],
  };
  return <ReactECharts option={option} style={{ height: 320 }} notMerge lazyUpdate />;
}
