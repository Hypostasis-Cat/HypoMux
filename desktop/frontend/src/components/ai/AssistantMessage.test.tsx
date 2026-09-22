// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AssistantMessage, summarizeReply } from "./AssistantMessage";
vi.mock("../../platform/desktop", () => ({ desktopPlatform: { openURL: vi.fn() } }));
afterEach(cleanup);
it("renders Markdown while excluding remote images and unsafe links", () => {
  const { container } = render(<AssistantMessage text={'**完成**\n\n- 已检查\n- `cs2.exe`\n\n[帮助](https://example.com) ![图片](https://example.com/a.png) [危险](javascript:alert)\n\n<script>alert(1)</script>'} />);
  expect(container.querySelector("strong")?.textContent).toBe("完成");
  expect(container.querySelectorAll("li")).toHaveLength(2);
  expect(container.querySelector("code")?.textContent).toBe("cs2.exe");
  expect(screen.getAllByRole("link")).toHaveLength(1);
  expect(container.querySelector("img,script")).toBeNull();
});
it("summarizes one sentence and bounds unpunctuated model output", () => {
  expect(summarizeReply("## 结论\n**已完成。**后面是详细说明。" )).toBe("已完成。");
  expect(summarizeReply("Finished. More details.")).toBe("Finished.");
  expect(Array.from(summarizeReply("长".repeat(500)))).toHaveLength(90);
});
