import type { Metadata, Viewport } from "next";
import { notFound } from "next/navigation";
import { hasLocale, NextIntlClientProvider } from "next-intl";
import { getTranslations, setRequestLocale } from "next-intl/server";
import type { CSSProperties, ReactNode } from "react";
import { auth } from "@/auth";
import { Providers } from "@/components/providers";
import { AppShell } from "@/components/shell/app-shell";
import { dirOf, routing } from "@/i18n/routing";
import { brandStyle } from "@/lib/brand";
import { fontVariables } from "@/lib/fonts";
import { daysToMandate } from "@/lib/mandate";
import { toShellFirm } from "@/lib/me";
import { fetchMe } from "@/lib/server/me";
import "../globals.css";

export async function generateMetadata({ params }: LayoutProps<"/[locale]">): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "Metadata" });
  return { title: { default: t("title"), template: `%s · ${t("title")}` }, description: t("description") };
}

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: dark)", color: "#0A0C0F" },
    { media: "(prefers-color-scheme: light)", color: "#F6F5F1" },
  ],
};

export default async function LocaleLayout({ children, params }: LayoutProps<"/[locale]">) {
  const { locale } = await params;
  if (!hasLocale(routing.locales, locale)) notFound();
  setRequestLocale(locale);

  const dir = dirOf(locale);
  const session = await auth();
  // Signed in: the Firm (name + white-label accent) comes from api-go /v1/me, server-side.
  const firm = session
    ? toShellFirm(await fetchMe(), (await getTranslations({ locale, namespace: "Shell" }))("firmFallback"))
    : null;
  const userName = session?.user?.name ?? session?.user?.email ?? undefined;

  return (
    <html lang={locale} dir={dir} className={fontVariables} suppressHydrationWarning>
      {/* Firm accent (validated + contrast-adjusted) scoped on <body> so portals inherit it. */}
      <body style={brandStyle(firm?.brandColor ?? null) as CSSProperties}>
        <NextIntlClientProvider>
          <Providers dir={dir} session={session}>
            {firm ? (
              <AppShell firm={firm} user={userName ? { name: userName } : undefined} mandateDays={daysToMandate()}>
                {children as ReactNode}
              </AppShell>
            ) : (
              // Signed out: pages render their own full-screen state (sign-in landing).
              (children as ReactNode)
            )}
          </Providers>
        </NextIntlClientProvider>
      </body>
    </html>
  );
}
