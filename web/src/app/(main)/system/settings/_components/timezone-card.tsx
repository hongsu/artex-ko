"use client";
import * as React from "react";

import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/lib/api";
import type { Settings } from "@/lib/types";
export function TimezoneCard() {
  const t = useTranslations("taskSchedule");
  const [settings, setSettings] = React.useState<Settings | null>(null);
  const [zone, setZone] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  React.useEffect(() => {
    api
      .settings()
      .then((s) => {
        setSettings(s);
        setZone(s.timezone_override ?? "");
      })
      .catch((e) => toast.error(e.message));
  }, []);
  const save = async (value: string) => {
    setBusy(true);
    try {
      const s = await api.setSettings({ timezone_override: value.trim() });
      setSettings(s);
      setZone(s.timezone_override ?? "");
      toast.success(t("timezoneSaved"));
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const source = settings?.timezone_source;
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("timezoneTitle")}</CardTitle>
        <CardDescription>{t("timezoneHint")}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <div className="grid gap-2">
          <Label htmlFor="timezone-env">{t("environment")}</Label>
          <Input id="timezone-env" readOnly value={settings?.timezone_env || t("unset")} />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="timezone-override">{t("override")}</Label>
          <Input
            id="timezone-override"
            placeholder={settings?.timezone_effective ?? "Asia/Seoul"}
            value={zone}
            onChange={(e) => setZone(e.target.value)}
            disabled={!settings || busy}
          />
        </div>
        <p className="text-muted-foreground text-sm">
          {t("effective")}: {settings?.timezone_effective ?? t("loading")} · {t("source")}:{" "}
          {source ? t(`${source}Source`) : t("loading")}
        </p>
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => void save(zone)} disabled={!settings || busy}>
            {t("save")}
          </Button>
          <Button variant="outline" onClick={() => void save("")} disabled={!settings || busy}>
            {t("useEnvironment")}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
