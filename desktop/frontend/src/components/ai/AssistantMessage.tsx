import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { desktopPlatform } from "../../platform/desktop";

export function AssistantMessage({ text }: { text: string }) {
  return <div className="ai-markdown"><ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml components={{
    img: ({ alt }) => <span>{alt || ""}</span>,
    a: ({ href, children }) => /^https?:\/\//i.test(href ?? "")
      ? <a href={href} onClick={event => { event.preventDefault(); if (href) void desktopPlatform.openURL(href); }}>{children}</a>
      : <span>{children}</span>,
  }}>{text}</ReactMarkdown></div>;
}

// A short, plain-text preview; the original Markdown always remains in history.
export function summarizeReply(text: string): string {
  const plain = text.replace(/```[\s\S]*?```/g, " ")
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/<[^>]*>/g, "")
    .replace(/^\s{0,3}(?:#{1,6}\s+|[-*>]\s+|\d+[.)]\s+)/gm, "")
    .replace(/[*_`~]/g, "")
    .split(/\n+/).map(line => line.trim()).filter(line => line && !/^(结论|摘要|总结|Summary|Conclusion)[:：]?$/i.test(line)).join(" ")
    .replace(/\s+/g, " ").trim();
  const sentence = plain.match(/^.*?[。！？!?]|^.*?\.(?:\s|$)/)?.[0]?.trim() || plain;
  const chars = Array.from(sentence);
  return chars.length > 90 ? chars.slice(0, 89).join("") + "…" : sentence;
}
