import { redirect } from "next/navigation";
import { getTranslations, setRequestLocale } from "next-intl/server";
import type { ReactNode } from "react";
import { auth } from "@/auth";
import { AppShell } from "@/components/shell/app-shell";
import { daysToMandate } from "@/lib/mandate";
import { toShellFirm } from "@/lib/me";
import { dashboardPath, signInPath } from "@/lib/routes";
import { fetchMe } from "@/lib/server/me";

export default async function AppLayout({ children, params }: { children: ReactNode; params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  setRequestLocale(locale);
  const session = await auth();
  // A layout cannot see the pathname; pages call requireSession() with their exact path (F5).
  if (!session) redirect(signInPath(locale, dashboardPath(locale)));
  const firm = toShellFirm(await fetchMe(), (await getTranslations({ locale, namespace: "Shell" }))("firmFallback"));
  const userName = session.user?.name ?? session.user?.email ?? undefined;
  return (
    <AppShell firm={firm} user={userName ? { name: userName } : undefined} mandateDays={daysToMandate()}>
      {children}
    </AppShell>
  );
}
