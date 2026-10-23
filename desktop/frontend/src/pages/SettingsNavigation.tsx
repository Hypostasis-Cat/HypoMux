import { Tab, TabList } from "@fluentui/react-components";
import { Bot20Regular, Document20Regular, Globe20Regular, PaintBrush20Regular, Settings20Regular, Shield20Regular } from "@fluentui/react-icons";
import { useEffect, useState } from "react";

const categories = ["general", "appearance", "network", "ai", "advanced", "configuration"] as const;
export type SettingsCategory = typeof categories[number];
type Translate = (zh: string, en: string) => string;

export function settingsCategories(text: Translate) {
  return [
    { id: "general", label: text("常规", "General"), description: text("语言、窗口与启动", "Language, window & startup"), icon: <Settings20Regular /> },
    { id: "appearance", label: text("外观", "Appearance"), description: text("主题、背景与动效", "Theme, background & motion"), icon: <PaintBrush20Regular /> },
    { id: "network", label: text("网络与 DNS", "Network & DNS"), description: text("代理端口与解析服务", "Proxy ports & resolvers"), icon: <Globe20Regular /> },
    { id: "ai", label: text("AI 助手", "AI Assistant"), description: text("功能开关与小 Mux", "Features & companion"), icon: <Bot20Regular /> },
    { id: "advanced", label: text("高级网络", "Advanced Network"), description: text("TUN、路由与兼容性", "TUN, routing & compatibility"), icon: <Shield20Regular /> },
    { id: "configuration", label: text("配置文件", "Configuration"), description: text("存储位置与旧版迁移", "Location & legacy migration"), icon: <Document20Regular /> },
  ] satisfies Array<{ id: SettingsCategory; label: string; description: string; icon: React.ReactNode }>;
}

function readCategory(): SettingsCategory {
  const requested = new URLSearchParams(window.location.search).get("settingsCategory");
  return categories.includes(requested as SettingsCategory) ? requested as SettingsCategory : "general";
}

export function useSettingsCategory() {
  const [category, setCategory] = useState(readCategory);
  useEffect(() => {
    const restore = () => setCategory(readCategory());
    window.addEventListener("popstate", restore);
    return () => window.removeEventListener("popstate", restore);
  }, []);
  const selectCategory = (next: SettingsCategory) => {
    if (next === category) return;
    const url = new URL(window.location.href);
    url.searchParams.set("settingsCategory", next);
    window.history.pushState(null, "", url);
    setCategory(next);
  };
  return [category, selectCategory] as const;
}

export function SettingsNavigation({ category, onSelect, networkDirty, text }: {
  category: SettingsCategory;
  onSelect: (category: SettingsCategory) => void;
  networkDirty: boolean;
  text: Translate;
}) {
  return (
    <nav className="settings-navigation page-tabs" aria-label={text("设置分类", "Settings categories")}>
      <TabList selectedValue={category} aria-label={text("设置分类", "Settings categories")}
        className="settings-category-tabs" onTabSelect={(_, data) => onSelect(data.value as SettingsCategory)}>
        {settingsCategories(text).map(item => (
          <Tab key={item.id} value={item.id} id={`settings-tab-${item.id}`} aria-controls="settings-category-panel"
            aria-label={item.label} icon={item.icon} className="settings-category-tab">
            <span className="settings-category-label">{item.label}{item.id === "network" && networkDirty && <span className="settings-dirty-dot" aria-hidden="true" title={text("有未保存的更改", "Unsaved changes")} />}</span>
          </Tab>
        ))}
      </TabList>
    </nav>
  );
}
