import {
  ChartLine,
  FileText,
  FlaskConical,
  House,
  ListChecks,
  Scale,
  Settings,
  Users,
  type LucideIcon,
} from "lucide-react";

export type NavKey =
  | "dashboard"
  | "clients"
  | "invoices"
  | "review"
  | "demo"
  | "legal"
  | "reports"
  | "settings";

export type NavItem = {
  key: NavKey;
  icon: LucideIcon;
  /** Locale-less pathname; `null` = not built yet (rendered disabled with a "Soon" hint). */
  href: string | null;
  /** Optional count shown in the badge slot (e.g. review queue size). */
  badge?: number;
};

/** Sidebar order follows the prototype Main board; the validation demo is Phase 0's working route. */
export const NAV: NavItem[] = [
  { key: "dashboard", icon: House, href: "/" },
  { key: "clients", icon: Users, href: null },
  { key: "invoices", icon: FileText, href: null },
  { key: "review", icon: ListChecks, href: null },
  { key: "demo", icon: FlaskConical, href: "/demo" },
  { key: "legal", icon: Scale, href: null },
  { key: "reports", icon: ChartLine, href: null },
  { key: "settings", icon: Settings, href: null },
];

export function isActive(href: string, pathname: string): boolean {
  return href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(`${href}/`);
}
