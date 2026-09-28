import { getToken } from "next-auth/jwt";

/**
 * Reads the Zitadel access token from the encrypted Auth.js session cookie, server-side
 * only (route handlers and server components). The token is never serialised to the client.
 */
export async function getAccessToken(headers: Headers): Promise<string | undefined> {
  const token = await getToken({
    req: { headers },
    secret: process.env.AUTH_SECRET,
    // Cookie name follows the scheme Auth.js uses (`__Secure-` prefix only on https).
    secureCookie: process.env.AUTH_URL?.startsWith("https://") ?? false,
  });
  if (!token?.accessToken) return undefined;
  if (token.accessTokenExpires && Date.now() >= token.accessTokenExpires * 1000) return undefined;
  return token.accessToken;
}
