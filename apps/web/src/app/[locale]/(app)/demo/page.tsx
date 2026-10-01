import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { DemoForm } from "@/components/demo/demo-form";
import { requireSession } from "@/lib/server/session";

export async function generateMetadata({ params }: PageProps<"/[locale]/demo">): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "Demo" });
  return { title: t("title") };
}

/** Auth-gated validation demo (prototype Review board styling). */
export default async function DemoPage({ params }: PageProps<"/[locale]/demo">) {
  const { locale } = await params;
  setRequestLocale(locale);
  await requireSession(locale, "/demo");

  return <DemoForm />;
}
