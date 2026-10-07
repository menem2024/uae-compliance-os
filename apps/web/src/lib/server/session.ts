import type { Session } from "next-auth";
import { redirect } from "next/navigation";
import { auth } from "@/auth";
import { signInPath } from "@/lib/routes";

/** Every (app) page calls this first with its own locale-less path (plan finding F5). */
export async function requireSession(locale: string, path: string): Promise<Session> {
  const session = await auth();
  if (!session) redirect(signInPath(locale, `/${locale}${path}`));
  return session;
}
