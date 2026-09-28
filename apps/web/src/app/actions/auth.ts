"use server";

import { signIn, signOut } from "@/auth";
import { routing } from "@/i18n/routing";
import { safeCallbackPath } from "@/lib/safe-callback";

function localeOf(formData: FormData): string {
  const l = String(formData.get("locale") ?? "");
  return (routing.locales as readonly string[]).includes(l) ? l : routing.defaultLocale;
}

/** Starts the Zitadel OIDC flow (PKCE + state cookies are set by Auth.js here). */
export async function signInAction(formData: FormData) {
  const locale = localeOf(formData);
  await signIn("zitadel", { redirectTo: safeCallbackPath(String(formData.get("callbackUrl") ?? ""), locale) });
}

export async function signOutAction(formData: FormData) {
  await signOut({ redirectTo: `/${localeOf(formData)}` });
}
