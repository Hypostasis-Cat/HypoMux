import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type PropsWithChildren,
} from "react";
import { appServices } from "../platform/services";
import legacyMessages from "./legacy.messages.json";

export type Locale = "zh" | "en";
type MessageValues = Record<string, string | number>;

type I18nContextValue = {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: (key: string, values?: MessageValues) => string;
};

const storageKey = "hypomux.language";
const messages = legacyMessages as Record<Locale, Record<string, string>>;
const I18nContext = createContext<I18nContextValue | null>(null);

const initialLocale = (): Locale =>
  window.localStorage.getItem(storageKey) === "en" ? "en" : "zh";

const formatMessage = (template: string, values?: MessageValues) => {
  if (!values) return template;
  return template.replace(/\{(\w+)(?::[^}]+)?\}/g, (placeholder, name: string) =>
    Object.prototype.hasOwnProperty.call(values, name) ? String(values[name]) : placeholder,
  );
};

export function LanguageProvider({ children }: PropsWithChildren) {
  const [locale, setLocaleState] = useState<Locale>(initialLocale);

  const setLocale = useCallback((next: Locale) => {
    setLocaleState(next);
    window.localStorage.setItem(storageKey, next);
  }, []);

  useEffect(() => {
    document.documentElement.lang = locale === "en" ? "en" : "zh-CN";
  }, [locale]);

  useEffect(() => {
    let active = true;
    void appServices.settings.get()
      .then((settings) => {
        if (active) setLocale(settings.language === "en" ? "en" : "zh");
      })
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, [setLocale]);

  const t = useCallback((key: string, values?: MessageValues) => {
    const template = messages[locale][key] ?? messages.zh[key] ?? key;
    return formatMessage(template, values);
  }, [locale]);

  const value = useMemo(() => ({ locale, setLocale, t }), [locale, setLocale, t]);
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n() {
  const context = useContext(I18nContext);
  if (!context) throw new Error("useI18n must be used inside LanguageProvider");
  return context;
}
