import { getTranslations, setRequestLocale } from "next-intl/server";
import type { ReactNode } from "react";
import { auth } from "@/auth";
import { AppShell } from "@/components/shell/app-shell";
import { daysToMandate } from "@/lib/mandate";
import { toShellFirm } from "@/lib/me";
import { fetchMe } from "@/lib/server/me";

export default async function AppLayout({ children, params }: { children: ReactNode; params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  setRequestLocale(locale);
  const session = await auth();
  // A layout cannot see the pathname, so it must not redirect itself (that would always send the
  // user back to /dashboard and lose the deep link). Every page calls requireSession() with its exact
  // path (F5) and redirects first; src/lib/app-pages-guard.test.ts enforces that no page forgets to.
  if (!session) return <>{children}</>;
  const firm = toShellFirm(await fetchMe(), (await getTranslations({ locale, namespace: "Shell" }))("firmFallback"));
  const userName = session.user?.name ?? session.user?.email ?? undefined;
  return (
    <AppShell firm={firm} user={userName ? { name: userName } : undefined} mandateDays={daysToMandate()}>
      {children}
    </AppShell>
  );
}
