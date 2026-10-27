import { useState } from "react";

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { describe, expect, it, vi } from "vitest";

import type { TaskScheduleSpec } from "@/lib/types";

import en from "../../messages/en.json";
import { TaskScheduleFields } from "./task-schedule-fields";

vi.mock("@/lib/api", () => ({
  api: {
    settings: vi.fn().mockResolvedValue({ timezone_effective: "UTC" }),
    previewTaskSchedule: vi.fn().mockResolvedValue({ next_run_at: "2026-10-28T09:00:00Z", timezone: "UTC" }),
  },
}));
function Form() {
  const [spec, setSpec] = useState<TaskScheduleSpec | null>(null);
  return (
    <NextIntlClientProvider locale="en" messages={en}>
      <TaskScheduleFields value={spec} onChange={setSpec} />
      <output data-testid="value">{JSON.stringify(spec)}</output>
    </NextIntlClientProvider>
  );
}
describe("schedule controls", () => {
  it("starts immediate and retains selected weekday/timezone for weekly execution", async () => {
    render(<Form />);
    expect(screen.getByTestId("value").textContent).toBe("null");
    await waitFor(() => expect(screen.getByLabelText("Execution")).toBeTruthy());
    fireEvent.change(screen.getByLabelText("Execution"), { target: { value: "weekly" } });
    fireEvent.change(screen.getByLabelText("Timezone"), { target: { value: "America/New_York" } });
    fireEvent.change(screen.getByLabelText("Weekday"), { target: { value: "1" } });
    const value = JSON.parse(screen.getByTestId("value").textContent!);
    expect(value).toMatchObject({ kind: "weekly", timezone: "America/New_York", weekday: 1 });
    fireEvent.change(screen.getByLabelText("Execution"), { target: { value: "immediate" } });
    expect(screen.getByTestId("value").textContent).toBe("null");
  });
});
