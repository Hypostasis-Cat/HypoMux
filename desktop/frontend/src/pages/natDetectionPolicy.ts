import type { EnginePhase } from "../state/useEngineState";

export const isNATDetectionBlocked = (phase?: EnginePhase) => (
  phase === "starting" || phase === "running" || phase === "degraded" || phase === "stopping"
);
