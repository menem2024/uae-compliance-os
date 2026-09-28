import { cn } from "@/lib/utils";

/** The eight-point geometric star mark (prototype), drawn in the Firm/platform brand colour. */
export function LogoMark({ className, size = 34 }: { className?: string; size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 40 40" aria-hidden="true" className={cn("shrink-0", className)}>
      <g fill="none" stroke="var(--brand)" strokeWidth="2">
        <rect x="9" y="9" width="22" height="22" rx="2" />
        <rect x="9" y="9" width="22" height="22" rx="2" transform="rotate(45 20 20)" />
      </g>
      <circle cx="20" cy="20" r="3.5" fill="var(--brand)" />
    </svg>
  );
}

export function Logo({ title, latin }: { title: string; latin: string }) {
  return (
    <div className="flex items-center gap-3 px-2 py-1">
      <LogoMark />
      <div className="flex flex-col gap-0.5">
        <span className="text-base leading-tight font-bold tracking-[-0.2px]">{title}</span>
        <span className="num text-[11px] leading-tight tracking-[1.5px] text-muted-foreground">{latin}</span>
      </div>
    </div>
  );
}
