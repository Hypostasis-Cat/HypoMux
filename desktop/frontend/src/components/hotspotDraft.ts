import type { HotspotConfig } from "../platform/services";

// Retain unsaved edits across toolkit navigation; never put secrets in web storage.
export const hotspotDraft: { current?: HotspotConfig } = {};
