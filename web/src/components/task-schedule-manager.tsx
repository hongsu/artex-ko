"use client";
import * as React from "react";

import Link from "next/link";

import { CalendarClockIcon } from "lucide-react";
import { useLocale, useTranslations } from "next-intl";
import { toast } from "sonner";

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import type { TaskSchedule, TaskScheduleRun } from "@/lib/types";

import { formatScheduleTime, TaskScheduleFields } from "./task-schedule-fields";

export function TaskScheduleManager() {
  const t = useTranslations("taskSchedule");
  const locale = useLocale();
  const [open, setOpen] = React.useState(false);
  const [rows, setRows] = React.useState<TaskSchedule[]>([]);
  const [loaded, setLoaded] = React.useState(false);
  const [editing, setEditing] = React.useState<TaskSchedule | null>(null);
  const [history, setHistory] = React.useState<{ row: TaskSchedule; runs: TaskScheduleRun[] } | null>(null);
  const [cancelID, setCancelID] = React.useState<number | null>(null);
  const [busy, setBusy] = React.useState(false);
  const refresh = React.useCallback(async () => {
    const items = await api.taskSchedules();
    setRows(items);
    setLoaded(true);
  }, []);
  React.useEffect(() => {
    if (!open) return;
    void refresh().catch((e) => toast.error(e.message));
    const timer = setInterval(() => void refresh().catch(() => undefined), 10000);
    return () => clearInterval(timer);
  }, [open, refresh]);
  const act = async (action: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await action();
      await refresh();
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const updateTask = (key: "name" | "description" | "goal", value: string) =>
    setEditing((row) => (row ? { ...row, task: { ...row.task, [key]: value } } : null));
  return (
    <>
      <Sheet
        open={open}
        onOpenChange={(value) => {
          setOpen(value);
          if (!value) {
            setEditing(null);
            setHistory(null);
          }
        }}
      >
        <SheetTrigger asChild>
          <Button variant="outline" size="sm">
            <CalendarClockIcon />
            {t("manage")}
          </Button>
        </SheetTrigger>
        <SheetContent className="w-full! max-w-none! sm:max-w-2xl! overflow-y-auto">
          <SheetHeader>
            <SheetTitle>{t("title")}</SheetTitle>
            <SheetDescription>{t("policy")}</SheetDescription>
          </SheetHeader>
          <div className="grid gap-4 px-4 pb-6">
            {editing ? (
              <form
                className="grid gap-4"
                onSubmit={(e) => {
                  e.preventDefault();
                  void act(async () => {
                    await api.updateTaskSchedule(editing.id, {
                      revision: editing.revision,
                      spec: editing.spec,
                      task: editing.task,
                      enabled: editing.enabled,
                    });
                    setEditing(null);
                    toast.success(t("saved"));
                  });
                }}
              >
                <Label htmlFor="schedule-name">{t("name")}</Label>
                <Input
                  id="schedule-name"
                  value={editing.task.name ?? ""}
                  onChange={(e) => updateTask("name", e.target.value)}
                />
                <Label htmlFor="schedule-description">{t("description")}</Label>
                <Textarea
                  id="schedule-description"
                  required
                  value={editing.task.description}
                  onChange={(e) => updateTask("description", e.target.value)}
                />
                <Label htmlFor="schedule-goal">{t("goal")}</Label>
                <Textarea
                  id="schedule-goal"
                  required
                  value={editing.task.goal}
                  onChange={(e) => updateTask("goal", e.target.value)}
                />
                <div className="flex items-center gap-2">
                  <Switch
                    id="schedule-enabled"
                    checked={editing.enabled}
                    onCheckedChange={(enabled) => setEditing({ ...editing, enabled })}
                  />
                  <Label htmlFor="schedule-enabled">{t("enabled")}</Label>
                </div>
                <TaskScheduleFields
                  allowImmediate={false}
                  value={editing.spec}
                  onChange={(spec) => {
                    if (spec) setEditing({ ...editing, spec });
                  }}
                />
                <div className="flex gap-2">
                  <Button type="submit" disabled={busy}>
                    {t("save")}
                  </Button>
                  <Button type="button" variant="outline" disabled={busy} onClick={() => setEditing(null)}>
                    {t("close")}
                  </Button>
                </div>
              </form>
            ) : history ? (
              <div className="grid gap-3">
                <div className="flex items-center justify-between">
                  <h3 className="font-medium">
                    {t("history")} · {history.row.task.name || `#${history.row.id}`}
                  </h3>
                  <Button variant="outline" onClick={() => setHistory(null)}>
                    {t("close")}
                  </Button>
                </div>
                {history.runs.length === 0 ? (
                  <p className="text-muted-foreground">{t("noHistory")}</p>
                ) : (
                  history.runs.map((run) => (
                    <div key={run.id} className="rounded-lg border p-3 text-sm">
                      <div>
                        {formatScheduleTime(run.scheduled_at, history.row.spec.timezone, locale)} (
                        {history.row.spec.timezone})
                      </div>
                      <div className="mt-2 flex flex-wrap items-center gap-3">
                        <Badge variant="outline">{t(run.outcome)}</Badge>
                        {run.task_id && (
                          <Link className="underline" href={`/function/tasks/detail?id=${run.task_id}`}>
                            {t("task")} #{run.task_id}
                          </Link>
                        )}
                      </div>
                    </div>
                  ))
                )}
              </div>
            ) : (
              <>
                {!loaded ? (
                  <p>{t("loading")}</p>
                ) : rows.length === 0 ? (
                  <p className="text-muted-foreground">{t("empty")}</p>
                ) : (
                  rows.map((row) => (
                    <div key={row.id} className="grid gap-3 rounded-lg border p-4">
                      <div className="flex items-center justify-between gap-2">
                        <h3 className="truncate font-medium">{row.task.name || row.task.description}</h3>
                        <Badge variant="outline">
                          {row.enabled ? t("enabled") : row.next_run_at ? t("paused") : t("finished")}
                        </Badge>
                      </div>
                      <p className="text-muted-foreground text-sm">
                        {t(row.spec.kind)} · {row.spec.time} · {row.spec.timezone}
                      </p>
                      {row.next_run_at && (
                        <p className="text-sm">
                          {t("next")}: {formatScheduleTime(row.next_run_at, row.spec.timezone, locale)}
                        </p>
                      )}
                      <div className="flex flex-wrap gap-2">
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={busy}
                          onClick={() => setEditing(structuredClone(row))}
                        >
                          {t("edit")}
                        </Button>
                        {(row.enabled || row.next_run_at || row.spec.kind !== "once") && (
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={busy}
                            onClick={() =>
                              void act(() =>
                                api.updateTaskSchedule(row.id, { enabled: !row.enabled, revision: row.revision }),
                              )
                            }
                          >
                            {row.enabled ? t("pause") : t("resume")}
                          </Button>
                        )}
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={busy}
                          onClick={() =>
                            void act(async () => setHistory({ row, runs: await api.taskScheduleRuns(row.id) }))
                          }
                        >
                          {t("history")}
                        </Button>
                        <Button size="sm" variant="destructive" disabled={busy} onClick={() => setCancelID(row.id)}>
                          {t("cancel")}
                        </Button>
                      </div>
                    </div>
                  ))
                )}
              </>
            )}
          </div>
        </SheetContent>
      </Sheet>
      <AlertDialog
        open={cancelID !== null}
        onOpenChange={(value) => {
          if (!value) setCancelID(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("cancel")}</AlertDialogTitle>
            <AlertDialogDescription>{t("cancelConfirm")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={busy}>{t("close")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={busy}
              onClick={(e) => {
                e.preventDefault();
                if (cancelID !== null)
                  void act(async () => {
                    await api.deleteTaskSchedule(cancelID);
                    setCancelID(null);
                  });
              }}
            >
              {t("cancel")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
