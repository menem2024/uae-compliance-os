import { dashboardPath } from "./routes";

export type LandingCta =
  | { testId: "sign-in"; href: null; callbackUrl: string }
  | { testId: "open-workspace"; href: string };

/**
 * The landing header's one piece of real logic: signed-out visitors get a
 * `signInAction` form target with a callback back to the dashboard; signed-in
 * visitors get a direct link into the workspace. Pure so the header stays thin.
 */
export function landingCta(signedIn: boolean, locale: string): LandingCta {
  if (signedIn) return { testId: "open-workspace", href: dashboardPath(locale) };
  return { testId: "sign-in", href: null, callbackUrl: dashboardPath(locale) };
}
