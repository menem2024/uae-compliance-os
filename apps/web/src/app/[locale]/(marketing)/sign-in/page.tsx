import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { auth } from "@/auth";
import { SignedOut } from "@/components/auth/signed-out";
import { dashboardPath } from "@/lib/routes";
import { safeCallbackPath } from "@/lib/safe-callback";

export async function generateMetadata({ params }: PageProps<"/[locale]/sign-in">): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "Metadata" });
  return { title: t("title") };
}

export default async function SignInPage({ params, searchParams }: PageProps<"/[locale]/sign-in">) {
  const { locale } = await params;
  setRequestLocale(locale);
  const { callbackUrl } = await searchParams;

  const target = callbackUrl ? safeCallbackPath(callbackUrl, locale) : dashboardPath(locale);

  const session = await auth();
  if (session) redirect(target);

  return <SignedOut locale={locale} callbackUrl={target} />;
}
