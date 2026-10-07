import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { AgentsView } from "@/components/agents/agents-view";
import { requireSession } from "@/lib/server/session";

type Props = { params: Promise<{ locale: string }> };

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "P1Agents" });
  return { title: t("title") };
}

export default async function AgentsPage({ params }: Props) {
  const { locale } = await params;
  setRequestLocale(locale);
  await requireSession(locale, "/agents");
  return <AgentsView />;
}
