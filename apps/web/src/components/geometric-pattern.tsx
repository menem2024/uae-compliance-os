import { useId } from "react";
import { cn } from "@/lib/utils";

/**
 * Faint Islamic geometric star tiling (adr/016: auth screens, empty states, hero
 * areas). Faded out with a radial mask so it is felt rather than shown.
 */
export function GeometricPattern({
  className,
  tile = 88,
  focus = "30% 30%",
}: {
  className?: string;
  tile?: number;
  focus?: string;
}) {
  const id = useId().replace(/:/g, "");
  const [cx, cy] = focus.split(" ");
  const c = tile / 2;
  const s = tile * 0.41;
  const o = c - s / 2;
  const arm = tile * 0.2;
  return (
    <svg aria-hidden="true" className={cn("pointer-events-none absolute inset-0 size-full", className)}>
      <defs>
        <pattern id={`star-${id}`} width={tile} height={tile} patternUnits="userSpaceOnUse">
          <g fill="none" stroke="var(--brand)" strokeWidth="0.8" opacity="0.35">
            <rect x={o} y={o} width={s} height={s} />
            <rect x={o} y={o} width={s} height={s} transform={`rotate(45 ${c} ${c})`} />
            <path d={`M0 ${c}h${arm}M${tile - arm} ${c}h${arm}M${c} 0v${arm}M${c} ${tile - arm}v${arm}`} />
          </g>
        </pattern>
        <radialGradient id={`fade-${id}`} cx={cx} cy={cy} r="75%">
          <stop offset="0" stopColor="white" stopOpacity="1" />
          <stop offset="1" stopColor="white" stopOpacity="0" />
        </radialGradient>
        <mask id={`mask-${id}`}>
          <rect width="100%" height="100%" fill={`url(#fade-${id})`} />
        </mask>
      </defs>
      <rect width="100%" height="100%" fill={`url(#star-${id})`} mask={`url(#mask-${id})`} />
    </svg>
  );
}
