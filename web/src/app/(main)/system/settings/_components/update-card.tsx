"use client";

import * as React from "react";

import {
  CheckCircle2Icon,
  DownloadIcon,
  ExternalLinkIcon,
  RefreshCwIcon,
  RotateCcwIcon,
  TriangleAlertIcon,
} from "lucide-react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Switch } from "@/components/ui/switch";
import { api, sseUrl } from "@/lib/api";
import type { UpdateCheck, UpdateProgress } from "@/lib/types";

/** Maximum time to wait for the new version to come up. An upgrade goes through three process starts (stage → swap → new version),
 *  each taking seconds; three minutes covers slow disks and Docker container rebuilds. */
const RESTART_TIMEOUT_MS = 180_000;

function humanSize(n?: number): string {
  if (!n || n <= 0) return "";
  const units = ["B", "KB", "MB", "GB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

export function UpdateCard() {
  const t = useTranslations("settingsPage");
  const richTags = {
    code: (chunks: React.ReactNode) => <span className="font-mono">{chunks}</span>,
  };
  const [info, setInfo] = React.useState<UpdateCheck | null>(null);
  const [checking, setChecking] = React.useState(true);
  const [progress, setProgress] = React.useState<UpdateProgress | null>(null);
  // Separate from progress: once staging completes the process is gone and SSE drops, so switch to polling /api/health.
  const [restarting, setRestarting] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const [toggling, setToggling] = React.useState(false);
  // Draft of the Settings value for the update source; "" means "use ARTEX_UPDATE_REPO or the default".
  const [repoDraft, setRepoDraft] = React.useState("");
  const [savingRepo, setSavingRepo] = React.useState(false);

  // quiet = the load on page entry: cached only, so opening Settings never contacts GitHub.
  // A manual "Check for updates" forces a fresh fetch (the backend refuses it unless updates are turned on).
  const check = React.useCallback(
    (quiet = false) => {
      setChecking(true);
      api
        .checkUpdate(quiet ? "cached" : "force")
        .then((r) => {
          setInfo(r);
          if (!quiet) {
            if (r.error) toast.error(t("update.toast.checkFailed", { error: r.error }));
            else if (r.has_update) toast.success(t("update.toast.found", { latest: r.latest ?? "" }));
            else if (r.comparable) toast.success(t("update.toast.upToDate"));
          }
        })
        .catch((e) => {
          if (!quiet) toast.error(t("update.toast.checkFailed", { error: (e as Error).message }));
        })
        .finally(() => setChecking(false));
    },
    [t],
  );

  React.useEffect(() => {
    check(true);
  }, [check]);

  // Poll /api/health until the version changes.
  //
  // The criterion must be "version changed", not "reachable": during the swap the old version briefly comes up once more
  // (only to put artex.new in place and exit immediately), so checking connectivity alone would misreport success.
  const waitForNewVersion = React.useCallback(
    async (fromVersion: string) => {
      setRestarting(true);
      const deadline = Date.now() + RESTART_TIMEOUT_MS;
      while (Date.now() < deadline) {
        await sleep(2000);
        try {
          const r = await fetch("/api/health", { cache: "no-store" });
          if (r.ok) {
            const j = (await r.json()) as { version?: string };
            if (j.version && j.version !== fromVersion) {
              toast.success(t("update.toast.updated", { version: j.version }));
              await sleep(800);
              window.location.reload();
              return;
            }
          }
        } catch {
          // Being unreachable during the restart window is expected; keep polling.
        }
      }
      setRestarting(false);
      toast.error(t("update.toast.restartTimeout"));
    },
    [t],
  );

  // Subscribe to update progress. SSE bypasses Next's /api rewrite (that layer buffers, so events wouldn't get through).
  const openStream = React.useCallback(
    (fromVersion: string) => {
      const es = new EventSource(sseUrl("/api/update/stream"));
      es.onmessage = (ev) => {
        let p: UpdateProgress;
        try {
          p = JSON.parse(ev.data) as UpdateProgress;
        } catch {
          return;
        }
        setProgress(p);
        if (p.phase === "failed") {
          es.close();
          setBusy(false);
          toast.error(t("update.toast.updateFailed", { error: p.error || p.message }));
          return;
        }
        if (p.phase === "staged") {
          es.close();
          void waitForNewVersion(fromVersion);
        }
      };
      es.onerror = () => {
        // SSE inevitably drops when the process exits. If we are already waiting for the restart this is normal;
        // let the /api/health polling decide.
        es.close();
      };
      return es;
    },
    [waitForNewVersion, t],
  );

  const toggleEnabled = (on: boolean) => {
    setToggling(true);
    api
      .setSettings({ update_check_enabled: on })
      .then(() => {
        setInfo((prev) => (prev ? { ...prev, enabled: on } : prev));
        check(true);
      })
      .catch((e) => toast.error(t("update.toast.toggleFailed", { error: (e as Error).message })))
      .finally(() => setToggling(false));
  };

  const enabled = info?.enabled ?? false;
  const savedRepo = info?.repo_setting ?? "";

  // Load the saved value into the field whenever the backend reports a different one.
  React.useEffect(() => {
    setRepoDraft(savedRepo);
  }, [savedRepo]);

  const saveRepo = (e: React.FormEvent) => {
    e.preventDefault();
    setSavingRepo(true);
    api
      .setSettings({ update_repo: repoDraft.trim() })
      .then(() => {
        toast.success(t("update.toast.sourceSaved"));
        check(true);
      })
      .catch((err) => toast.error(t("update.toast.sourceFailed", { error: (err as Error).message })))
      .finally(() => setSavingRepo(false));
  };

  const doUpdate = () => {
    if (!info) return;
    const from = info.current;
    const ok = window.confirm(
      `${t("update.confirm.title", { latest: info.latest ?? "" })}\n\n` +
        t("update.confirm.restart") +
        (info.mode === "docker" ? `\n\n${t("update.confirm.docker")}` : ""),
    );
    if (!ok) return;

    setBusy(true);
    setProgress({ phase: "downloading", percent: 0, message: t("update.toast.preparing") });
    const es = openStream(from);
    api.applyUpdate().catch((e) => {
      es.close();
      setBusy(false);
      setProgress(null);
      toast.error(t("update.toast.startFailed", { error: (e as Error).message }));
    });
  };

  const doRollback = () => {
    if (!info) return;
    if (!window.confirm(t("update.confirm.rollback"))) return;
    const from = info.current;
    setBusy(true);
    api
      .rollbackUpdate()
      .then(() => {
        toast.success(t("update.toast.rollbackSwitched"));
        void waitForNewVersion(from);
      })
      .catch((e) => {
        setBusy(false);
        toast.error(t("update.toast.rollbackFailed", { error: (e as Error).message }));
      });
  };

  const phase = progress?.phase;
  const showProgress = busy || restarting;
  // Only the download phase has a real percentage (from Content-Length). Verify/extract/waiting for restart all have
  // unknown duration, so the bar fills and pulses to mean "busy, but can't say for how long".
  const downloading = !restarting && phase === "downloading";
  const pct = downloading ? Math.max(progress?.percent ?? 0, 0) : 100;

  return (
    // The settings page is a multi-column masonry layout; cards handle their own row spacing and avoid column breaks (see the comment in page.tsx).
    <Card className="mb-4 break-inside-avoid md:mb-6">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <DownloadIcon className="size-4" />
          {t("update.title")}
        </CardTitle>
        <CardDescription>{t("update.desc")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex items-center justify-between gap-4">
          <Label htmlFor="update-check-enabled" className="text-sm font-normal text-muted-foreground">
            {enabled ? t("update.enabledOn") : t("update.enabledOff")}
          </Label>
          <Switch
            id="update-check-enabled"
            aria-describedby="update-check-enabled-description"
            checked={enabled}
            disabled={!info || toggling || busy || restarting}
            onCheckedChange={toggleEnabled}
          />
        </div>
        <p id="update-check-enabled-description" className="text-xs text-muted-foreground">
          {t.rich("update.source", {
            ...richTags,
            repo: info?.repo || "…",
            env: info?.repo_env || "ARTEX_UPDATE_REPO",
          })}
        </p>
        {info && !enabled && <p className="text-xs text-muted-foreground">{t("update.disabledNote")}</p>}

        <form onSubmit={saveRepo} className="space-y-1.5">
          <Label htmlFor="update-repo" className="text-sm font-normal">
            {t("update.sourceLabel")}
          </Label>
          <div className="flex gap-2">
            <Input
              id="update-repo"
              aria-describedby="update-repo-help"
              className="font-mono"
              placeholder="owner/name"
              autoComplete="off"
              spellCheck={false}
              value={repoDraft}
              onChange={(e) => setRepoDraft(e.target.value)}
              disabled={!info || savingRepo || busy || restarting}
            />
            <Button
              type="submit"
              variant="outline"
              size="sm"
              className="h-9"
              disabled={!info || savingRepo || busy || restarting || repoDraft.trim() === savedRepo}
            >
              {t("update.sourceSave")}
            </Button>
          </div>
          <p id="update-repo-help" className="text-xs text-muted-foreground">
            {t.rich("update.sourceHelp", {
              ...richTags,
              env: info?.repo_env || "ARTEX_UPDATE_REPO",
              default: "sounil/artex-english",
            })}
          </p>
        </form>

        <div className="flex flex-wrap items-center gap-2 text-sm">
          <span className="text-muted-foreground">{t("update.currentVersion")}</span>
          <Badge variant="secondary" className="font-mono">
            {info?.current ?? "…"}
          </Badge>
          {info && (
            <>
              <Badge variant="outline" className="font-mono">
                {info.os}/{info.arch}
              </Badge>
              <Badge variant="outline">
                {info.mode === "docker" ? t("update.modeDocker") : t("update.modeStandalone")}
              </Badge>
            </>
          )}
          {info?.latest && (
            <>
              <span className="text-muted-foreground">{t("update.latestVersion")}</span>
              <Badge variant={info.has_update ? "default" : "secondary"} className="font-mono">
                {info.latest}
              </Badge>
            </>
          )}
          {info?.html_url && (
            <a
              href={info.html_url}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1 text-xs text-muted-foreground underline-offset-4 hover:underline"
            >
              {t("update.changelog")} <ExternalLinkIcon className="size-3" />
            </a>
          )}
        </div>

        {info?.boot_notice && (
          <p className="flex items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 p-2 text-xs text-amber-700 dark:text-amber-400">
            <TriangleAlertIcon className="mt-0.5 size-3.5 shrink-0" />
            {info.boot_notice}
          </p>
        )}

        {info?.error && (
          <p className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 p-2 text-xs text-destructive">
            <TriangleAlertIcon className="mt-0.5 size-3.5 shrink-0" />
            {t("update.connectError", { error: info.error })}
          </p>
        )}

        {info && !info.comparable && info.reason && <p className="text-xs text-muted-foreground">{info.reason}</p>}

        {info?.has_update && info.asset_available === false && (
          <p className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 p-2 text-xs text-destructive">
            <TriangleAlertIcon className="mt-0.5 size-3.5 shrink-0" />
            {t("update.noAsset", { latest: info.latest ?? "", os: info.os, arch: info.arch, asset: info.asset ?? "" })}
          </p>
        )}

        {info?.has_update && info.asset_available !== false && (
          <p className="text-xs text-muted-foreground">
            {t("update.willDownloadPrefix")}
            <span className="font-mono">{info.asset}</span>
            {info.size ? t("update.willDownloadSize", { size: humanSize(info.size) }) : ""}
            {t("update.willDownloadSuffix")}
          </p>
        )}

        {info && !info.has_update && info.comparable && !info.error && (
          <p className="flex items-center gap-2 text-xs text-muted-foreground">
            <CheckCircle2Icon className="size-3.5 text-emerald-600" />
            {t("update.upToDate")}
          </p>
        )}

        {info?.mode === "docker" && info.has_update && (
          <p className="text-xs text-muted-foreground">{t.rich("update.dockerNote", richTags)}</p>
        )}

        {showProgress && (
          <div className="space-y-1.5">
            <Progress value={pct} className={downloading ? undefined : "animate-pulse"} />
            <p className="text-xs text-muted-foreground">{restarting ? t("update.restarting") : progress?.message}</p>
          </div>
        )}

        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => check(false)}
            disabled={!enabled || checking || busy || restarting}
          >
            <RefreshCwIcon className={checking ? "size-4 animate-spin" : "size-4"} />
            {t("update.check")}
          </Button>
          <Button
            size="sm"
            onClick={doUpdate}
            disabled={!enabled || busy || restarting || !info?.has_update || info?.asset_available === false}
          >
            <DownloadIcon className="size-4" />
            {info?.has_update ? t("update.updateToVersion", { latest: info.latest ?? "" }) : t("update.updateNow")}
          </Button>
          {info?.has_backup && (
            <Button variant="ghost" size="sm" onClick={doRollback} disabled={busy || restarting}>
              <RotateCcwIcon className="size-4" />
              {t("update.rollback")}
            </Button>
          )}
        </div>

        <p className="text-xs text-muted-foreground">{t.rich("update.footer", richTags)}</p>
      </CardContent>
    </Card>
  );
}
