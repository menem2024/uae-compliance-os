/**
 * Fixture data for scripts/seed-demo.sh. Everything here is fictional and synthetic: the company names,
 * the TRNs (15 digits, first digit 1, last two digits 03, derived from the name so they are stable) and
 * the buyers. Do not replace them with real companies.
 */

export type Emirate = "AUH" | "DXB" | "SHJ" | "UAQ" | "FUJ" | "AJM" | "RAK";

export type SeedClient = {
  /** Short code used as the invoice-number prefix, for example "SAH-2026-0001". */
  code: string;
  name: string;
  nameAr: string;
  emirate: Emirate;
  trn: string;
};

export type SeedBuyer = { name: string; trn: string; city: string; emirate: Emirate };

export const CITY: Record<Emirate, string> = {
  AUH: "Abu Dhabi",
  DXB: "Dubai",
  SHJ: "Sharjah",
  UAQ: "Umm Al Quwain",
  FUJ: "Fujairah",
  AJM: "Ajman",
  RAK: "Ras Al Khaimah",
};

/** FNV-1a, 32 bit. Stable across runs and platforms; only used to derive fixture digits. */
function fnv1a(s: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h >>> 0;
}

/** 15-digit synthetic TRN: leading 1, twelve digits derived from the seed, trailing 03. */
export function syntheticTrn(seed: string): string {
  const a = String(fnv1a(`${seed}|a`)).padStart(10, "0");
  const b = String(fnv1a(`${seed}|b`)).padStart(10, "0");
  return `1${(a + b).slice(0, 12)}03`;
}

type ClientDef = Omit<SeedClient, "trn">;

const CLIENTS_A: ClientDef[] = [
  { code: "SAH", name: "Sahari Logistics FZE", nameAr: "سحاري للخدمات اللوجستية", emirate: "SHJ" },
  { code: "DAF", name: "Dar Al Fajr Building Materials LLC", nameAr: "دار الفجر لمواد البناء ذ.م.م", emirate: "AJM" },
  { code: "ZAW", name: "Zahrat Al Wadi Catering LLC", nameAr: "زهرة الوادي للتموين ذ.م.م", emirate: "RAK" },
  { code: "BAH", name: "Burj Al Hikma Consultancy FZ-LLC", nameAr: "برج الحكمة للاستشارات", emirate: "DXB" },
  // Arabic-script primary names: the name field itself is Arabic.
  { code: "RAK", name: "رمال الخليج للتجارة العامة ش.ذ.م.م", nameAr: "رمال الخليج للتجارة العامة ش.ذ.م.م", emirate: "DXB" },
  { code: "GHI", name: "Gulf Horizon Interiors LLC", nameAr: "أفق الخليج للديكور الداخلي", emirate: "AUH" },
  { code: "FCA", name: "Falcon Crest Auto Spare Parts LLC", nameAr: "قمة الصقر لقطع غيار السيارات", emirate: "UAQ" },
  { code: "JDT", name: "الجزيرة للتمور والفواكه المجففة", nameAr: "الجزيرة للتمور والفواكه المجففة", emirate: "FUJ" },
  { code: "MSS", name: "Madar Smart Systems Technology LLC", nameAr: "مدار للأنظمة الذكية", emirate: "DXB" },
  { code: "WNM", name: "شركة واحة النور للمستلزمات الطبية ذ.م.م", nameAr: "شركة واحة النور للمستلزمات الطبية ذ.م.م", emirate: "AUH" },
];

const CLIENTS_B: ClientDef[] = [
  { code: "BWM", name: "Bluewave Marine Services LLC", nameAr: "الموجة الزرقاء للخدمات البحرية", emirate: "DXB" },
  { code: "DRB", name: "Desert Rose Boutique FZE", nameAr: "وردة الصحراء للأزياء", emirate: "SHJ" },
];

const withTrn = (firm: string, defs: ClientDef[]): SeedClient[] =>
  defs.map((d) => ({ ...d, trn: syntheticTrn(`${firm}|${d.code}`) }));

export const FIRM_A_CLIENTS: SeedClient[] = withTrn("A", CLIENTS_A);
export const FIRM_B_CLIENTS: SeedClient[] = withTrn("B", CLIENTS_B);

const BUYER_DEFS: { name: string; city: string; emirate: Emirate }[] = [
  { name: "Noor Electronics LLC", city: "Abu Dhabi", emirate: "AUH" },
  { name: "Al Marsa Hospitality Group LLC", city: "Dubai", emirate: "DXB" },
  { name: "Emirates Pearl Retail FZ-LLC", city: "Dubai", emirate: "DXB" },
  { name: "Qasr Al Rimal Real Estate LLC", city: "Sharjah", emirate: "SHJ" },
  { name: "Khaleej Fresh Markets LLC", city: "Ajman", emirate: "AJM" },
  { name: "Najm Al Sahra Contracting LLC", city: "Al Ain", emirate: "AUH" },
  { name: "Oasis Care Clinics LLC", city: "Ras Al Khaimah", emirate: "RAK" },
  { name: "Horizon Tech Solutions FZCO", city: "Dubai", emirate: "DXB" },
  { name: "Bait Al Tijara Trading Est.", city: "Fujairah", emirate: "FUJ" },
  { name: "Lulu Bay Seafood LLC", city: "Umm Al Quwain", emirate: "UAQ" },
  { name: "Safwa Education Services LLC", city: "Abu Dhabi", emirate: "AUH" },
  { name: "Mawj Event Management LLC", city: "Sharjah", emirate: "SHJ" },
];

export const BUYERS: SeedBuyer[] = BUYER_DEFS.map((b) => ({ ...b, trn: syntheticTrn(`buyer|${b.name}`) }));

export type Product = { name: string; description: string; unit: string; priceTenths: number[] };

/** Goods and services; prices are in tenths of a dirham so line bases stay exact decimals. */
export const PRODUCTS: Product[] = [
  { name: "Pallet freight, Jebel Ali to Sharjah", description: "Per pallet, palletised cargo", unit: "H87", priceTenths: [450, 620, 780] },
  { name: "Portland cement 50 kg bag", description: "Grade 42.5", unit: "H87", priceTenths: [145, 160, 172] },
  { name: "Office paper A4, box of 5 reams", description: "80 gsm", unit: "H87", priceTenths: [980, 1120, 1250] },
  { name: "Consulting hours, VAT advisory", description: "Senior consultant", unit: "HUR", priceTenths: [3500, 4200, 5000] },
  { name: "Event catering per guest", description: "Buffet, three courses", unit: "H87", priceTenths: [850, 1100, 1350] },
  { name: "Interior fit-out labour", description: "Per working day", unit: "DAY", priceTenths: [4800, 5500, 6200] },
  { name: "Brake pad set, front axle", description: "OEM equivalent", unit: "H87", priceTenths: [2200, 3100, 3800] },
  { name: "Medjool dates 5 kg carton", description: "Premium grade", unit: "H87", priceTenths: [1950, 2400, 2800] },
  { name: "Managed IT support, monthly", description: "Up to 25 devices", unit: "H87", priceTenths: [9500, 12000, 14500] },
  { name: "Disposable nitrile gloves, box of 100", description: "Medical grade", unit: "H87", priceTenths: [320, 380, 450] },
];
