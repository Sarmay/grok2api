const dateTimeFormatters = new Map<string, Intl.DateTimeFormat>();
const compactDateTimeFormatters = new Map<string, Intl.DateTimeFormat>();
const numberFormatters = new Map<string, Intl.NumberFormat>();
const tokenMillionFormatters = new Map<string, Intl.NumberFormat>();

export function formatDateTime(value: string | null | undefined, locale: string): string {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "-";
  }
  let formatter = dateTimeFormatters.get(locale);
  if (!formatter) {
    formatter = new Intl.DateTimeFormat(locale, {
      dateStyle: "medium",
      timeStyle: "short",
    });
    dateTimeFormatters.set(locale, formatter);
  }
  return formatter.format(date);
}

export function formatCompactDateTime(value: string | null | undefined, locale: string): string {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "-";
  }
  let formatter = compactDateTimeFormatters.get(locale);
  if (!formatter) {
    formatter = new Intl.DateTimeFormat(locale, { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" });
    compactDateTimeFormatters.set(locale, formatter);
  }
  const parts = formatter.formatToParts(date);
  return `${datePart(parts, "year")}/${datePart(parts, "month")}/${datePart(parts, "day")} ${datePart(parts, "hour")}:${datePart(parts, "minute")}:${datePart(parts, "second")}`;
}

function datePart(parts: Intl.DateTimeFormatPart[], type: Intl.DateTimeFormatPartTypes): string {
  return parts.find((part) => part.type === type)?.value ?? "--";
}

export function formatNumber(value: number, locale: string, maximumFractionDigits = 2): string {
  const key = `${locale}:${maximumFractionDigits}`;
  let formatter = numberFormatters.get(key);
  if (!formatter) {
    formatter = new Intl.NumberFormat(locale, { maximumFractionDigits });
    numberFormatters.set(key, formatter);
  }
  return formatter.format(value);
}

const tokenUnits = [
  { threshold: 1_000_000_000_000, suffix: "t" },
  { threshold: 1_000_000_000, suffix: "b" },
  { threshold: 1_000_000, suffix: "m" },
  { threshold: 1_000, suffix: "k" },
] as const;

/** Compact token counts as k / m / b / t. Values under 1,000 stay exact. */
export function formatCompactTokens(value: number, locale: string): string {
  if (!Number.isFinite(value)) return formatNumber(0, locale, 0);
  const sign = value < 0 ? "-" : "";
  const absolute = Math.abs(value);
  const unit = tokenUnits.find((candidate) => absolute >= candidate.threshold);
  if (!unit) return formatNumber(value, locale, 0);
  const digits = tokenFractionDigits(absolute / unit.threshold);
  let scaled = roundTo(absolute / unit.threshold, digits);
  let suffix: string = unit.suffix;
  if (scaled >= 1_000) {
    const promoted = tokenUnits.find((candidate) => candidate.threshold === unit.threshold * 1_000);
    if (promoted) {
      scaled = roundTo(scaled / 1_000, tokenFractionDigits(scaled / 1_000));
      suffix = promoted.suffix;
    }
  }
  return `${sign}${formatNumber(scaled, locale, tokenFractionDigits(scaled))}${suffix}`;
}

function tokenFractionDigits(scaled: number): number {
  if (scaled >= 100) return 0;
  if (scaled >= 10) return 1;
  return 2;
}

function roundTo(value: number, digits: number): number {
  const factor = 10 ** digits;
  return Math.round(value * factor) / factor;
}

/** Uses M for large token counts while keeping small values readable. */
export function formatTokenMillions(value: number, locale: string): string {
  const normalized = Number.isFinite(value) ? Math.max(0, value) : 0;
  if (normalized < 1_000) return formatNumber(normalized, locale, 0);
  let formatter = tokenMillionFormatters.get(locale);
  if (!formatter) {
    formatter = new Intl.NumberFormat(locale, { maximumSignificantDigits: 3 });
    tokenMillionFormatters.set(locale, formatter);
  }
  return `${formatter.format(normalized / 1_000_000)}M`;
}

export function formatDuration(milliseconds: number): string {
  if (milliseconds < 1000) {
    return `${milliseconds} ms`;
  }
  return `${(milliseconds / 1000).toFixed(milliseconds < 10000 ? 2 : 1)} s`;
}

export function toDateTimeLocal(value: string | null | undefined): string {
  if (!value) {
    return "";
  }
  const date = new Date(value);
  const offset = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offset).toISOString().slice(0, 19);
}
