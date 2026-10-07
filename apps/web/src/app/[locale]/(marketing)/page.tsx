import { ArrowRight } from "lucide-react";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { auth } from "@/auth";
import { signInAction } from "@/app/actions/auth";
import { Hero } from "@/components/marketing/hero";
import { HowItWorks } from "@/components/marketing/how-it-works";
import { LandingHeader } from "@/components/marketing/landing-header";
import { OrchestraGraph } from "@/components/marketing/orchestra-graph";
import { TrustSection } from "@/components/marketing/trust-section";
import { landingCta } from "@/lib/landing-cta";
import { daysToMandate } from "@/lib/mandate";

/**
 * Public landing: hero, mandate countdown, the animated agent-orchestra graph,
 * "how it works", trust points and a closing CTA. No AppShell, no authenticated
 * API calls — the header's sign-in-or-open-workspace CTA is the only real logic
 * (`lib/landing-cta.ts`), repeated here at the foot of the page.
 */
export default async function MarketingPage({ params }: PageProps<"/[locale]">) {
  const { locale } = await params;
  setRequestLocale(locale);

  const session = await auth();
  const signedIn = Boolean(session);
  const tCta = await getTranslations("P1Landing.cta");
  const tNav = await getTranslations("Shell.nav");
  const cta = landingCta(signedIn, locale);

  return (
    <div className="relative flex min-h-dvh flex-col bg-background text-foreground">
      <LandingHeader locale={locale} signedIn={signedIn} />
      <main className="flex flex-1 flex-col gap-16 px-6 pb-20 md:px-10">
        <Hero serverDays={daysToMandate()} />
        <OrchestraGraph />
        <HowItWorks />
        <TrustSection />
        <section className="lift flex flex-col items-start gap-4 rounded-xl border bg-panel p-8 text-start md:p-10">
          <h2 className="text-2xl font-semibold tracking-[-0.3px]">{tCta("title")}</h2>
          <p className="max-w-xl text-[15px] leading-7 text-muted-foreground">{tCta("body")}</p>
          {/*
            Only the header carries data-testid="sign-in" / "open-workspace" (those ids must
            stay unique on the page — e2e/lib/auth.ts:57-59 does a strict `getByTestId(...).click()`).
            This footer CTA repeats the same destination without repeating the test id.
          */}
          {cta.testId === "open-workspace" ? (
            <a
              href={cta.href}
              className="group inline-flex h-11 items-center gap-2.5 rounded-lg bg-primary px-5 text-sm font-bold text-primary-foreground transition-[filter,transform] hover:brightness-110 active:translate-y-px"
            >
              {tNav("dashboard")}
              <ArrowRight
                className="size-4 transition-transform group-hover:translate-x-0.5 rtl:-scale-x-100 rtl:group-hover:-translate-x-0.5"
                strokeWidth={2}
                aria-hidden
              />
            </a>
          ) : (
            <form action={signInAction}>
              <input type="hidden" name="locale" value={locale} />
              <input type="hidden" name="callbackUrl" value={cta.callbackUrl} />
              <button
                type="submit"
                className="group inline-flex h-11 items-center gap-2.5 rounded-lg bg-primary px-5 text-sm font-bold text-primary-foreground transition-[filter,transform] hover:brightness-110 active:translate-y-px"
              >
                {tCta("button")}
                <ArrowRight
                  className="size-4 transition-transform group-hover:translate-x-0.5 rtl:-scale-x-100 rtl:group-hover:-translate-x-0.5"
                  strokeWidth={2}
                  aria-hidden
                />
              </button>
            </form>
          )}
        </section>
      </main>
    </div>
  );
}
