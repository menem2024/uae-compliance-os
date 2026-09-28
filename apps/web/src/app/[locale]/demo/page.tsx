import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { auth } from "@/auth";
import { DemoForm } from "@/components/demo/demo-form";

export async function generateMetadata({ params }: PageProps<"/[locale]/demo">): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "Demo" });
  return { title: t("title") };
}

/** Auth-gated validation demo (prototype Review board styling). */
export default async function DemoPage({ params }: PageProps<"/[locale]/demo">) {
  const { locale } = await params;
  setRequestLocale(locale);

  const session = await auth();
  if (!session) {
    // The signed-out landing offers sign-in and returns here afterwards.
    redirect(`/${locale}?callbackUrl=${encodeURIComponent(`/${locale}/demo`)}`);
  }

  return <DemoForm />;
}
