import NextAuth from "next-auth";
import Zitadel from "next-auth/providers/zitadel";

declare module "next-auth/jwt" {
  interface JWT {
    /** Zitadel access token: lives only in the encrypted, httpOnly session cookie. */
    accessToken?: string;
    /** Access-token expiry, epoch seconds. */
    accessTokenExpires?: number;
  }
}

export const { handlers, auth, signIn, signOut } = NextAuth({
  providers: [
    Zitadel({
      // One hostname for browser and server (http://zitadel.localhost:8085); no Host overrides.
      issuer: process.env.ZITADEL_ISSUER,
      clientId: process.env.ZITADEL_CLIENT_ID,
      clientSecret: process.env.ZITADEL_CLIENT_SECRET,
      authorization: { params: { scope: "openid profile email urn:zitadel:iam:user:resourceowner" } },
    }),
  ],
  session: { strategy: "jwt" },
  callbacks: {
    async jwt({ token, account }) {
      if (account?.access_token) {
        token.accessToken = account.access_token;
        token.accessTokenExpires = account.expires_at;
      }
      // No refresh in Phase 0: an expired access token ends the session (null signs out).
      if (token.accessTokenExpires && Date.now() >= token.accessTokenExpires * 1000) return null;
      return token;
    },
    // accessToken is intentionally NOT copied into the session: browser JS must never see it.
    async session({ session }) {
      return { expires: session.expires, user: { name: session.user?.name, email: session.user?.email } } as typeof session;
    },
  },
});
