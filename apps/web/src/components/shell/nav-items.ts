import {
  Bot, ChartLine, FileText, FileUp, FlaskConical, House, ListChecks, Scale, Settings, Users, type LucideIcon,
} from "lucide-react";

export type NavItem = {
  /** Unique across tracks. */
  key: string;
  icon: LucideIcon;
  /** Locale-less pathname; `null` = not built yet (rendered disabled with a "Soon" hint). */
  href: string | null;
  /** Full message path, in the owning track's namespace (e.g. "P1Nav.documents"). */
  labelKey: string;
  /** Top-bar heading for the page; defaults to labelKey. */
  titleKey?: string;
  subtitleKey?: string;
  badge?: number;
  /** Pulsing "live" dot (the agent activity page). */
  live?: boolean;
};

/** Sidebar order. Tracks append their own items; only the owning track edits an item. */
export const NAV_ITEMS: NavItem[] = [
  { key: "dashboard", icon: House, href: "/dashboard", labelKey: "Shell.nav.dashboard",
    titleKey: "Dashboard.title", subtitleKey: "Dashboard.subtitle" },
  { key: "clients", icon: Users, href: "/clients", labelKey: "Shell.nav.clients",
    titleKey: "P1Clients.title", subtitleKey: "P1Clients.subtitle" },
  { key: "documents", icon: FileUp, href: null, labelKey: "P1Nav.documents" },
  { key: "agents", icon: Bot, href: null, labelKey: "P1Nav.agents", live: true },
  { key: "invoices", icon: FileText, href: null, labelKey: "Shell.nav.invoices" },
  { key: "review", icon: ListChecks, href: null, labelKey: "Shell.nav.review" },
  { key: "demo", icon: FlaskConical, href: "/demo", labelKey: "Shell.nav.demo",
    titleKey: "Demo.title", subtitleKey: "DemoPage.subtitle" },
  { key: "legal", icon: Scale, href: null, labelKey: "Shell.nav.legal" },
  { key: "reports", icon: ChartLine, href: null, labelKey: "Shell.nav.reports" },
  { key: "settings", icon: Settings, href: "/settings", labelKey: "Shell.nav.settings",
    titleKey: "P1Settings.title", subtitleKey: "P1Settings.subtitle" },
];

export function isActive(href: string, pathname: string): boolean {
  return pathname === href || pathname.startsWith(`${href}/`);
}

export function activeNavItem(pathname: string): NavItem | null {
  return NAV_ITEMS.find((n) => n.href !== null && isActive(n.href, pathname)) ?? null;
}
