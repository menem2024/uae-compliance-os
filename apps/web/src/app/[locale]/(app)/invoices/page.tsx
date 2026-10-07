import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { InvoicesView } from "@/components/invoices/invoices-view";
import { requireSession } from "@/lib/server/session";

type Props = { params: Promise<{ locale: string }> };

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "P2Invoices" });
  return { title: t("title") };
}

/** This Firm's invoices plus the "New demo invoice" sample picker. */
export default async function InvoicesPage({ params }: Props) {
  const { locale } = await params;
  setRequestLocale(locale);
  await requireSession(locale, "/invoices");
  return <InvoicesView />;
}
