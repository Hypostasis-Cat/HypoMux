import {
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Image,
  Link,
  Spinner,
  Toast,
  Toaster,
  ToastBody,
  ToastTitle,
  useId,
  useToastController,
} from "@fluentui/react-components";
import {
  ArrowSync20Regular,
  Code20Regular,
} from "@fluentui/react-icons";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { GlassSurface } from "../components/material/GlassSurface";
import { desktopPlatform } from "../platform/desktop";
import { appServices, type UpdateCheckResult } from "../platform/services";
import { productInfo } from "../product";
import { useState } from "react";
import { useI18n } from "../i18n/i18n";

function ReleaseNotes({ notes, emptyText }: { notes?: string; emptyText: string }) {
  const value = notes?.trim() || emptyText;
  return (
    <div className="update-notes">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        skipHtml
        components={{
          a: ({ href, children }) => (
            <Link
              href={href}
              onClick={(event) => {
                event.preventDefault();
                if (href) void desktopPlatform.openURL(href);
              }}
            >
              {children}
            </Link>
          ),
        }}
      >
        {value}
      </ReactMarkdown>
    </div>
  );
}

export function AboutPage() {
  const { locale, t } = useI18n();
  const text = (zh: string, en: string) => locale === "en" ? en : zh;
  const [checking, setChecking] = useState(false);
  const [downloading, setDownloading] = useState(false);
  const [downloadPercent, setDownloadPercent] = useState(0);
  const [update, setUpdate] = useState<UpdateCheckResult | null>(null);
  const [updateDialogOpen, setUpdateDialogOpen] = useState(false);
  const toasterId = useId("about-toaster");
  const { dispatchToast } = useToastController(toasterId);

  const notify = (title: string, body: string, intent: "success" | "error" | "info" = "info") => {
    dispatchToast(
      <Toast><ToastTitle>{title}</ToastTitle><ToastBody>{body}</ToastBody></Toast>,
      { intent, timeout: 3600 },
    );
  };

  const checkForUpdates = async () => {
    setChecking(true);
    try {
      const result = await appServices.updater.check();
      if (result.available) {
        setUpdate(result);
        setUpdateDialogOpen(true);
      } else {
        setUpdateDialogOpen(false);
        setUpdate(null);
        notify(
          text("已是最新版本", "You're up to date"),
          text(`当前版本：v${result.current_version}`, `Current version: v${result.current_version}`),
          "success",
        );
      }
    } catch (error) {
      notify(text("检查更新失败", "Update check failed"), String(error), "error");
    } finally {
      setChecking(false);
    }
  };

  const installUpdate = async () => {
    if (!update) return;
    setDownloading(true);
    setDownloadPercent(0);
    const progressTimer = window.setInterval(() => {
      void appServices.updater.progress().then((progress) => {
        if (progress.total > 0) {
          setDownloadPercent(Math.min(99, Math.floor(progress.downloaded * 100 / progress.total)));
        }
      }).catch(() => undefined);
    }, 250);
    try {
      const path = await appServices.updater.download(update.release);
      setDownloadPercent(100);
      await appServices.updater.launchInstaller(path);
      notify(text("下载完成", "Download complete"), t("about_update_installing"), "success");
      window.setTimeout(() => desktopPlatform.quit(), 250);
    } catch (error) {
      notify(text("下载安装包失败", "Installer download failed"), String(error), "error");
      setDownloading(false);
    } finally {
      window.clearInterval(progressTimer);
    }
  };

  return (
    <main className="about-page">
      <Toaster toasterId={toasterId} position="top-end" />
      <header className="page-heading">
        <div>
          <span className="section-kicker">Open-source multi-link accelerator</span>
          <h1>{text("关于 HypoMux", "About HypoMux")}</h1>
          <p>{t("about_intro")}</p>
        </div>
      </header>

      <div className="about-layout">
        <GlassSurface className="about-product">
          <div className="about-brand">
            <Image src="/support/icon.ico" alt={text("HypoMux 图标", "HypoMux icon")} className="about-app-icon" />
            <div>
              <h2>{productInfo.name}</h2>
              <strong>{text("当前版本", "Current version")}: v{productInfo.version}</strong>
              <span>{productInfo.build}</span>
            </div>
          </div>
          <p>{t("about_intro")}</p>
          <div className="about-actions">
            <Button appearance="primary" icon={<Code20Regular />} onClick={() => desktopPlatform.openURL(productInfo.repository)}>
              GitHub
            </Button>
            <Button appearance="primary" icon={checking ? <Spinner size="tiny" /> : <ArrowSync20Regular />} disabled={checking} onClick={checkForUpdates}>
              {checking ? t("about_checking_update") : t("about_check_update")}
            </Button>
          </div>
        </GlassSurface>

        <GlassSurface className="about-copy">
          <h2>{t("about_notice_title")}</h2>
          <p>{t("about_notice_text")}</p>
        </GlassSurface>

        <GlassSurface className="signpath-section">
          <Image src="/support/SignPath/SignPath.png" alt="SignPath" className="signpath-logo" />
          <div>
            <h2>{t("about_signpath_title")}</h2>
            <p>
              {text("Windows 代码签名由", "Free Windows code signing is provided by")}{" "}
              <Link onClick={() => desktopPlatform.openURL("https://signpath.io/")}>SignPath.io</Link>
              {text(" 免费提供，证书由 ", "; the certificate is issued by ")}
              <Link onClick={() => desktopPlatform.openURL("https://signpath.org/")}>SignPath Foundation</Link>
              {text(" 颁发。感谢他们对 HypoMux 与开源软件的支持。", ". Thank you for supporting HypoMux and open-source software.")}
            </p>
            <p>
              {text(
                "HypoMux 的官方 Windows 发布版本均由此仓库的 GitHub Actions 构建，并提交至 SignPath 进行代码签名。请仅从官方 GitHub Releases 页面下载安装包，并确认已签名版本的发布者显示为 SignPath Foundation。",
                "Official HypoMux Windows releases are built by this repository's GitHub Actions and submitted to SignPath for code signing. Download installers only from the official GitHub Releases page and verify that the signed publisher is SignPath Foundation.",
              )}
            </p>
          </div>
        </GlassSurface>

        <GlassSurface className="sponsor-section">
          <h2>{t("about_sponsorship_title")}</h2>
          <p>{t("settings_sponsorship_text")}</p>
          <div className="payment-grid">
            <figure className="payment-item">
              <figcaption>{t("about_wechat")}</figcaption>
              <Image src="/support/wei.png" alt={text("微信支付二维码", "WeChat Pay QR code")} />
              <span>{text("微信赞赏（请备注您的昵称，未备注则表示匿名）", "WeChat sponsorship (leave your nickname; blank means anonymous)")}</span>
            </figure>
            <figure className="payment-item">
              <figcaption>{t("about_alipay")}</figcaption>
              <Image src="/support/zhi.jpg" alt={text("支付宝二维码", "Alipay QR code")} />
              <span>{text("支付宝赞赏（请备注您的昵称，未备注则表示匿名）", "Alipay sponsorship (leave your nickname; blank means anonymous)")}</span>
            </figure>
          </div>
        </GlassSurface>
      </div>

      <Dialog
        open={updateDialogOpen}
        onOpenChange={(_, data) => {
          if (!data.open && !downloading) setUpdateDialogOpen(false);
        }}
      >
        <DialogSurface className="update-dialog">
          <DialogBody>
            <DialogTitle>{t("about_update_available_title")}</DialogTitle>
            <DialogContent>
              <p className="update-summary">
                {text("当前版本", "Current version")}: v{update?.current_version}<br />
                {text("最新版本", "Latest version")}: v{update?.release.tag_name.replace(/^v/i, "")}
              </p>
              <strong>{t("about_update_notes_label")}</strong>
              <ReleaseNotes notes={update?.release.notes} emptyText={t("about_update_notes_empty")} />
              <p>{t("about_update_download_hint")}</p>
            </DialogContent>
            <DialogActions>
              <Button disabled={downloading} onClick={() => setUpdateDialogOpen(false)}>{t("about_update_later")}</Button>
              <Button appearance="primary" disabled={downloading} icon={downloading ? <Spinner size="tiny" /> : undefined} onClick={installUpdate}>
                {downloading ? t("about_update_downloading", { percent: downloadPercent }) : t("about_update_now")}
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </main>
  );
}
