import { readFileSync } from "node:fs";
import { join } from "node:path";
import { createTranslator } from "use-intl/core";
import { describe, expect, it } from "vitest";

const load = (l: string) => JSON.parse(readFileSync(join(process.cwd(), `messages/${l}.json`), "utf8"));
const tr = (l: string) => createTranslator({ locale: l, messages: load(l), namespace: "P2Invoices.list" });

describe("error and warning count plurals", () => {
  it("English uses singular for 1", () => {
    const t = tr("en");
    expect(t("errorsCount", { count: 1 })).toBe("1 error");
    expect(t("errorsCount", { count: 2 })).toBe("2 errors");
    expect(t("warningsCount", { count: 1 })).toBe("1 warning");
    expect(t("warningsCount", { count: 0 })).toBe("0 warnings");
  });

  it("Arabic has all six forms and Western digits", () => {
    const t = tr("ar");
    const s = (count: number) => t("errorsCount", { count });
    expect(s(0)).toBe("لا أخطاء");
    expect(s(1)).toBe("خطأ واحد");
    expect(s(2)).toBe("خطآن");
    expect(s(3)).toBe("3 أخطاء");
    expect(s(11)).toBe("11 خطأً");
    expect(s(100)).toBe("100 خطأ");
    expect(t("warningsCount", { count: 1 })).toBe("تحذير واحد");
    expect(t("warningsCount", { count: 2 })).toBe("تحذيران");
    expect(t("warningsCount", { count: 7 })).toBe("7 تحذيرات");
    expect(t("warningsCount", { count: 12 })).toBe("12 تحذيراً");
  });
});
