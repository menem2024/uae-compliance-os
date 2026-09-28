/**
 * Western digits in both locales (adr/016). Never format numbers with an Arabic
 * locale such as `ar-AE`, which yields Arabic-Indic digits.
 */
const integer = new Intl.NumberFormat("en-US");

export function formatInt(n: number): string {
  return integer.format(n);
}
