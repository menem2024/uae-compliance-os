import { createNavigation } from "next-intl/navigation";
import { routing } from "./routing";

/** Locale-aware Link / router / pathname (pathnames are locale-less, e.g. "/demo"). */
export const { Link, redirect, usePathname, useRouter, getPathname } = createNavigation(routing);
