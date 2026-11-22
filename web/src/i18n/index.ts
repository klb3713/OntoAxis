import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import type { SupportedLocale } from "../config/runtime";
import { en } from "./en";
import { zh } from "./zh";

export const SUPPORTED_LOCALES: SupportedLocale[] = ["zh-CN", "en"];
export const LOCALE_STORAGE_KEY = "ontoaxis.web.locale";

function readInitialLocale(): SupportedLocale {
  try {
    const stored = window.localStorage.getItem(LOCALE_STORAGE_KEY);
    if (stored === "zh-CN" || stored === "en") return stored;
  } catch {
    // localStorage 不可用时使用默认中文。
  }
  return "zh-CN";
}

export const i18nResources = {
  "zh-CN": { translation: zh },
  en: { translation: en },
} as const;

void i18n.use(initReactI18next).init({
  resources: i18nResources,
  lng: readInitialLocale(),
  fallbackLng: "en",
  supportedLngs: SUPPORTED_LOCALES,
  interpolation: { escapeValue: false },
  returnNull: false,
});

export function setLocale(locale: SupportedLocale): Promise<unknown> {
  try {
    window.localStorage.setItem(LOCALE_STORAGE_KEY, locale);
  } catch {
    // 忽略存储失败，语言切换仍然生效。
  }
  document.documentElement.lang = locale;
  return i18n.changeLanguage(locale);
}

export function currentLocale(): SupportedLocale {
  return i18n.language === "en" ? "en" : "zh-CN";
}

export default i18n;
