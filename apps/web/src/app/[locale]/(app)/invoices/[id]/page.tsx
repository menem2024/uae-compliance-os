import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { ReviewView } from "@/components/invoices/review-view";
import { requireSession } from "@/lib/server/session";

type Props = { params: Promise<{ locale: string; id: string }> };

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "P2Invoices.review" });
  return { title: t("title") };
}

/** The guided review of one invoice: issues, corrections, re-validate, approve, export, audit trail. */
export default async function InvoiceReviewPage({ params }: Props) {
  const { locale, id } = await params;
  setRequestLocale(locale);
  await requireSession(locale, `/invoices/${encodeURIComponent(id)}`);
  return <ReviewView id={id} />;
}
