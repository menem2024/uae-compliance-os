import type { Metadata, Viewport } from "next";
import { notFound } from "next/navigation";
import { hasLocale, NextIntlClientProvider } from "next-intl";
import { getTranslations, setRequestLocale } from "next-intl/server";
import type { CSSProperties, ReactNode } from "react";
import { Providers } from "@/components/providers";
import { AppShell } from "@/components/shell/app-shell";
import type { ShellFirm } from "@/components/shell/types";
import { dirOf, routing } from "@/i18n/routing";
import { brandStyle } from "@/lib/brand";
import { fontVariables } from "@/lib/fonts";
import { daysToMandate } from "@/lib/mandate";
import "../globals.css";

/**
 * Static placeholder Firm until Auth.js is wired (story 7b). 7b replaces this with
 * the `/v1/me` result fetched through the BFF helper when a session exists:
 *   { name: me.firm_name, brandColor: me.brand_color }
 */
const DEMO_FIRM: ShellFirm = { name: "Demo Firm", brandColor: null };

export function generateStaticParams() {
  return routing.locales.map((locale) => ({ locale }));
}

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
  const firm = DEMO_FIRM;

  return (
    <html lang={locale} dir={dir} className={fontVariables} suppressHydrationWarning>
      {/* Firm accent (validated + contrast-adjusted) scoped on <body> so portals inherit it. */}
      <body style={brandStyle(firm.brandColor) as CSSProperties}>
        <NextIntlClientProvider>
          <Providers dir={dir}>
            <AppShell firm={firm} mandateDays={daysToMandate()}>
              {children as ReactNode}
            </AppShell>
          </Providers>
        </NextIntlClientProvider>
      </body>
    </html>
  );
}
