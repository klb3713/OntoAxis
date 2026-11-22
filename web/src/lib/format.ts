import { currentLocale } from "../i18n";

export function formatDateTime(value: string | undefined): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(currentLocale() === "en" ? "en-US" : "zh-CN", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

export function initials(value: string): string {
  return value.trim().slice(0, 1).toUpperCase() || "?";
}
