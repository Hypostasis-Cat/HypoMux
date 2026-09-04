import type { RoutingSnapshot } from "../platform/services";

export type RoutingApplyState =
  | "hot_reloaded"
  | "restart_required"
  | "next_tun_start"
  | "inactive_mode";

export const routingApplyState = (
  snapshot: Pick<RoutingSnapshot, "restart_required">,
  phase: string,
  mode: string,
): RoutingApplyState => {
  if (mode !== "tun") return "inactive_mode";
  if (phase !== "running" && phase !== "degraded") return "next_tun_start";
  return snapshot.restart_required ? "restart_required" : "hot_reloaded";
};
