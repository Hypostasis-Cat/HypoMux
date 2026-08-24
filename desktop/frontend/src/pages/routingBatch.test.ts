import { describe, expect, it } from "vitest";
import { parseRoutingBatchValues, routingRuleIdentity } from "./routingBatch";

describe("parseRoutingBatchValues", () => {
  it("accepts line, whitespace, and Chinese punctuation separators for domains", () => {
    expect(parseRoutingBatchValues("one.example，two.example\nthree.example; four.example", "domain"))
      .toEqual(["one.example", "two.example", "three.example", "four.example"]);
  });

  it("keeps spaces inside process names", () => {
    expect(parseRoutingBatchValues("My App, Preview.exe\nhelper;test.exe", "process"))
      .toEqual(["My App, Preview.exe", "helper;test.exe"]);
  });

  it("uses normalized rule identity semantics", () => {
    expect(routingRuleIdentity("domain", "Example.COM"))
      .toBe(routingRuleIdentity("domain", "example.com"));
  });
});
