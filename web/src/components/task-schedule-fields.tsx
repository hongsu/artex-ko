"use client";
import * as React from "react";

import { useLocale, useTranslations } from "next-intl";

import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { api } from "@/lib/api";
import type { TaskScheduleSpec } from "@/lib/types";

export function formatScheduleTime(value: string, zone: string, locale: string) {
  try {
    return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short", timeZone: zone }).format(
      new Date(value),
    );
  } catch {
    return value;
  }
}
export function TaskScheduleFields({
  value,
  onChange,
  allowImmediate = true,
}: {
  value: TaskScheduleSpec | null;
  onChange: (value: TaskScheduleSpec | null) => void;
  allowImmediate?: boolean;
}) {
  const t = useTranslations("taskSchedule");
  const locale = useLocale();
  const id = React.useId();
  const [defaultZone, setDefaultZone] = React.useState("");
  const [preview, setPreview] = React.useState<{ next_run_at: string; timezone: string } | null>(null);
  const [error, setError] = React.useState(false);
  React.useEffect(() => {
    let alive = true;
    api
      .settings()
      .then((s) => {
        if (alive) setDefaultZone(s.timezone_effective ?? "");
      })
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, []);
  React.useEffect(() => {
    let alive = true;
    setPreview(null);
    setError(false);
    if (!value) return;
    const timer = setTimeout(() => {
      api
        .previewTaskSchedule(value)
        .then((r) => {
          if (alive) setPreview(r);
        })
        .catch(() => {
          if (alive) setError(true);
        });
    }, 250);
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [value]);
  const patch = (changes: Partial<TaskScheduleSpec>) => {
    if (value) onChange({ ...value, ...changes });
  };
  const weekdays = Array.from({ length: 7 }, (_, n) =>
    new Intl.DateTimeFormat(locale, { weekday: "long", timeZone: "UTC" }).format(new Date(Date.UTC(2026, 9, 25 + n))),
  );
  return (
    <div className="grid gap-3 rounded-lg border p-4">
      <Label htmlFor={`${id}-kind`}>{t("execution")}</Label>
      <NativeSelect
        id={`${id}-kind`}
        value={value?.kind ?? "immediate"}
        onChange={(e) =>
          onChange(
            e.target.value === "immediate"
              ? null
              : {
                  ...value,
                  kind: e.target.value as TaskScheduleSpec["kind"],
                  time: value?.time ?? "09:00",
                  weekday: value?.weekday ?? 1,
                  timezone: value?.timezone ?? defaultZone,
                },
          )
        }
      >
        {allowImmediate && <NativeSelectOption value="immediate">{t("immediate")}</NativeSelectOption>}
        {(["once", "daily", "weekly"] as const).map((k) => (
          <NativeSelectOption key={k} value={k}>
            {t(k)}
          </NativeSelectOption>
        ))}
      </NativeSelect>
      {value && (
        <>
          {value.kind === "once" && (
            <div className="grid gap-2">
              <Label htmlFor={`${id}-date`}>{t("date")}</Label>
              <Input
                id={`${id}-date`}
                type="date"
                value={value.date ?? ""}
                onChange={(e) => patch({ date: e.target.value })}
              />
            </div>
          )}
          {value.kind === "weekly" && (
            <div className="grid gap-2">
              <Label htmlFor={`${id}-day`}>{t("weekday")}</Label>
              <NativeSelect
                id={`${id}-day`}
                value={value.weekday}
                onChange={(e) => patch({ weekday: Number(e.target.value) })}
              >
                {weekdays.map((day, n) => (
                  <NativeSelectOption key={day} value={n}>
                    {day}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </div>
          )}
          <div className="grid gap-2">
            <Label htmlFor={`${id}-time`}>{t("time")}</Label>
            <Input id={`${id}-time`} type="time" value={value.time} onChange={(e) => patch({ time: e.target.value })} />
          </div>
          <div className="grid gap-2">
            <Label htmlFor={`${id}-zone`}>{t("timezone")}</Label>
            <Input
              id={`${id}-zone`}
              value={value.timezone}
              placeholder={defaultZone}
              onChange={(e) => patch({ timezone: e.target.value })}
              list={`${id}-zones`}
            />
            <datalist id={`${id}-zones`}>
              {[
                "Asia/Seoul",
                "UTC",
                "Asia/Tokyo",
                "America/New_York",
                "America/Los_Angeles",
                "Europe/London",
                "Europe/Berlin",
              ].map((z) => (
                <option key={z} value={z} />
              ))}
            </datalist>
            <p className="text-muted-foreground text-xs">{t("defaultZone", { zone: defaultZone || t("loading") })}</p>
          </div>
          <p aria-live="polite" className={error ? "text-destructive text-sm" : "text-muted-foreground text-sm"}>
            {preview
              ? `${t("next")}: ${formatScheduleTime(preview.next_run_at, preview.timezone, locale)} (${preview.timezone})`
              : error
                ? t("previewError")
                : t("loading")}
          </p>
          <p className="text-muted-foreground text-xs">{t("policy")}</p>
        </>
      )}
    </div>
  );
}
