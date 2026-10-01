import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { FirmSettingsForm } from "@/components/settings/firm-settings-form";
import { requireSession } from "@/lib/server/session";

type Props = { params: Promise<{ locale: string }> };

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "P1Settings" });
  return { title: t("title") };
}

export default async function SettingsPage({ params }: Props) {
  const { locale } = await params;
  setRequestLocale(locale);
  await requireSession(locale, "/settings");
  return <FirmSettingsForm />;
}
