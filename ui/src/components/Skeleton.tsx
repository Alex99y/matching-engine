import type { CSSProperties } from "react";

interface SkeletonProps {
  width?: string | number;
  height?: string | number;
  borderRadius?: string | number;
  style?: CSSProperties;
}

export function Skeleton({
  width = "100%",
  height = 14,
  borderRadius = "var(--radius-sm)",
  style,
}: SkeletonProps) {
  return (
    <div
      style={{
        width,
        height,
        borderRadius,
        background:
          "linear-gradient(90deg, var(--bg-card) 25%, var(--bg-hover) 50%, var(--bg-card) 75%)",
        backgroundSize: "200% 100%",
        animation: "shimmer 1.4s infinite linear",
        flexShrink: 0,
        ...style,
      }}
    />
  );
}

export function SkeletonRows({ count, gap = 6 }: { count: number; gap?: number }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap }}>
      {Array.from({ length: count }, (_, i) => (
        <Skeleton key={i} height={18} />
      ))}
    </div>
  );
}
