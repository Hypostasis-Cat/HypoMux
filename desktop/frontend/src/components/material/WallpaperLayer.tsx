import { useAppearance } from "../../theme/appearance.store";
import { resolveWallpaperBackground } from "../../theme/wallpaper";

export function WallpaperLayer() {
  const { settings } = useAppearance();
  const useNativeMica = settings.backgroundSource === "system" && settings.material === "mica";
  const background = resolveWallpaperBackground(settings);

  return (
    <div className={`wallpaper-layer${useNativeMica ? " is-native-mica" : ""}`} aria-hidden="true">
      {!useNativeMica && <div className="wallpaper-image" style={{ background }} />}
      <div className="wallpaper-overlay" />
      <div className="wallpaper-noise" />
    </div>
  );
}
