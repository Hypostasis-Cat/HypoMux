export const ROUTING_BATCH_MAX_VALUES = 2000;

export type RoutingBatchMatchType = "process" | "domain" | "ip";

export const parseRoutingBatchValues = (input: string, matchType: RoutingBatchMatchType): string[] => {
  const separator = matchType === "process"
    ? /[\r\n\t]+/
    : /[\s,，;；]+/;
  return input
    .split(separator)
    .map((value) => value.trim())
    .filter(Boolean);
};

export const routingRuleIdentity = (matchType: string, value: string) =>
  `${matchType}\u0000${value.toLocaleLowerCase()}`;
