import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import type { SupabaseClient } from "@supabase/supabase-js";
import { Dashboard } from "./Dashboard";
import { fetchBuckets, openStream, ForbiddenError } from "../api";
import type { Severity, Snapshot, State } from "../types";

vi.mock("../api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api")>()),
  fetchBuckets: vi.fn(),
  openStream: vi.fn(),
}));

const PHONE_WIDTH = 360;
const NODE_FS = "node:fs";

async function readStylesheet(): Promise<string> {
  const fs = (await import(NODE_FS)) as { readFileSync(path: string, encoding: "utf8"): string };
  return fs.readFileSync("src/styles.css", "utf8");
}
const PHONE_GUTTER = 48;

function bucket(id: string, title: string, severity: Severity, state: State = "live"): Snapshot {
  return { id, title, state, severity, headline: "", updatedAt: new Date().toISOString(), data: {} };
}

const buckets = [
  bucket("usage", "Usage", "ok"),
  bucket("cost", "Cost", "critical"),
  bucket("security", "Security", "warn"),
  bucket("logs", "Logs", "ok", "source_down"),
];

function renderDashboard(onSignOut = vi.fn(), initial = "/") {
  render(
    <MemoryRouter initialEntries={[initial]}>
      <Dashboard supabase={{} as SupabaseClient} onSignOut={onSignOut} ownerEmail="owner@example.com" />
    </MemoryRouter>,
  );
  return { onSignOut };
}

function sideNav() {
  return screen.getByRole("navigation", { name: "Buckets" });
}

function linkNames(nav: HTMLElement) {
  return within(nav)
    .getAllByRole("link")
    .map((link) => link.textContent);
}

function pxValues(text: string) {
  return [...text.matchAll(/(\d+(?:\.\d+)?)px/g)].map((match) => Number(match[1]));
}

function withoutMinWrapped(value: string) {
  return value.replace(/\bmin\([^()]*\)/g, "");
}

function fixedTrackWidths(value: string) {
  const outsideMinmax = withoutMinWrapped(value).replace(/minmax\(([^,()]*),[^()]*\)/g, " ");
  const minmaxFloors = [...withoutMinWrapped(value).matchAll(/minmax\(([^,()]*),/g)].map((match) => match[1]);
  return [...pxValues(outsideMinmax).map((px) => ({ px, isFixedTrack: true })), ...minmaxFloors.flatMap(pxValues).map((px) => ({ px, isFixedTrack: false }))];
}

function widthRulesTooWideForPhone(css: string) {
  const offenders: string[] = [];
  for (const rule of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const selector = rule[1].trim();
    for (const declaration of rule[2].matchAll(/(?:^|;)\s*([\w-]+)\s*:\s*([^;]+)/g)) {
      const [, property, value] = declaration;
      if (property === "grid-template-columns") {
        const tooWide = fixedTrackWidths(value).some(
          ({ px, isFixedTrack }) => isFixedTrack || px > PHONE_WIDTH - PHONE_GUTTER,
        );
        if (tooWide) offenders.push(`${selector} { ${property}: ${value.trim()} }`);
      }
      if (property === "width" || property === "min-width" || property === "flex-basis") {
        if (pxValues(withoutMinWrapped(value)).some((px) => px > PHONE_WIDTH - PHONE_GUTTER)) {
          offenders.push(`${selector} { ${property}: ${value.trim()} }`);
        }
      }
    }
  }
  return offenders;
}

function classesFixedWiderThanPhone(root: HTMLElement) {
  const tailwindPx = (token: string) => {
    const arbitrary = token.match(/^(?:w|min-w|basis)-\[(\d+)px\]$/);
    if (arbitrary) return Number(arbitrary[1]);
    const scale = token.match(/^(?:w|min-w|basis)-(\d+(?:\.\d+)?)$/);
    return scale ? Number(scale[1]) * 4 : 0;
  };
  return [root, ...root.querySelectorAll<HTMLElement>("*")]
    .flatMap((element) => [...element.classList])
    .filter((token) => !token.includes(":") && tailwindPx(token) > PHONE_WIDTH - PHONE_GUTTER);
}

beforeEach(() => {
  vi.mocked(fetchBuckets).mockReset();
  vi.mocked(openStream).mockReset().mockReturnValue(new Promise<void>(() => {}));
});

describe("Dashboard load", () => {
  it("loads the buckets into the nav and the overview, and reports the connection live", async () => {
    vi.mocked(fetchBuckets).mockResolvedValue(buckets);
    renderDashboard();

    expect(await within(sideNav()).findByRole("link", { name: "Cost" })).toBeInTheDocument();
    expect(screen.getByLabelText("Open Usage")).toBeInTheDocument();
    expect(screen.getAllByText("live").length).toBeGreaterThan(0);
    expect(openStream).toHaveBeenCalled();
  });

  it("shows the API-unavailable message when the first load fails", async () => {
    vi.mocked(fetchBuckets).mockRejectedValue(new Error("api/buckets: HTTP 502"));
    renderDashboard();

    expect(await screen.findByText("Overseer API unavailable.")).toBeInTheDocument();
    expect(screen.getAllByText("error").length).toBeGreaterThan(0);
  });
});

describe("forbidden screen", () => {
  it("shows the not-the-owner screen without the shell, and signs out from it", async () => {
    vi.mocked(fetchBuckets).mockRejectedValue(new ForbiddenError());
    const { onSignOut } = renderDashboard();

    expect(await screen.findByText("This account is not the owner. Access denied.")).toBeInTheDocument();
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(onSignOut).toHaveBeenCalledTimes(1);
  });
});

describe("nav order", () => {
  it("lists Overview first, then buckets worst first", async () => {
    vi.mocked(fetchBuckets).mockResolvedValue(buckets);
    renderDashboard();

    await within(sideNav()).findByRole("link", { name: "Cost" });
    expect(linkNames(sideNav())).toEqual(["▦Overview", "Cost", "Security", "Logs", "Usage"]);
  });
});

describe("phone drawer", () => {
  it("opens the nav as a dialog and closes it on Escape", async () => {
    vi.mocked(fetchBuckets).mockResolvedValue(buckets);
    renderDashboard();
    await within(sideNav()).findByRole("link", { name: "Cost" });

    await userEvent.click(screen.getByRole("button", { name: "Open navigation" }));
    const drawer = screen.getByRole("dialog");
    expect(within(drawer).getByRole("link", { name: "Security" })).toBeInTheDocument();

    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Open navigation" })).toHaveFocus();
  });

  it("closes on route change and lands on the chosen bucket", async () => {
    vi.mocked(fetchBuckets).mockResolvedValue(buckets);
    renderDashboard();
    await within(sideNav()).findByRole("link", { name: "Cost" });

    await userEvent.click(screen.getByRole("button", { name: "Open navigation" }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("link", { name: "Security" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(within(sideNav()).getByRole("link", { name: "Security" })).toHaveAttribute("aria-current", "page");
  });

  it("closes the drawer from its own close button", async () => {
    vi.mocked(fetchBuckets).mockResolvedValue(buckets);
    renderDashboard();

    await userEvent.click(screen.getByRole("button", { name: "Open navigation" }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Close navigation" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });
});

describe("keyboard reach", () => {
  it("reaches every nav item by Tab, each carrying a visible focus ring", async () => {
    vi.mocked(fetchBuckets).mockResolvedValue(buckets);
    renderDashboard();
    await within(sideNav()).findByRole("link", { name: "Cost" });

    const reached = new Set<Element>();
    for (let step = 0; step < 40; step++) {
      await userEvent.tab();
      if (document.activeElement && document.activeElement !== document.body) reached.add(document.activeElement);
    }

    const navLinks = within(sideNav()).getAllByRole("link");
    for (const link of navLinks) expect(reached).toContain(link);
    const shellControls = [...reached].filter((control) => !screen.getByRole("main").contains(control));
    expect(shellControls.length).toBeGreaterThan(navLinks.length);
    for (const control of shellControls) expect(control.className).toMatch(/focus-visible:outline-accent/);
  });

  it("keeps collapsed nav items reachable by name", async () => {
    vi.mocked(fetchBuckets).mockResolvedValue(buckets);
    renderDashboard();
    await within(sideNav()).findByRole("link", { name: "Cost" });

    const toggle = screen.getByRole("button", { name: "Collapse navigation" });
    await userEvent.click(toggle);

    expect(screen.getByRole("button", { name: "Expand navigation" })).toHaveAttribute("aria-expanded", "false");
    expect(linkNames(sideNav())).toEqual(["▦Overview", "Cost", "Security", "Logs", "Usage"]);
  });
});

describe("no sideways scroll at 360px", () => {
  it("renders a phone-width page with nothing forced wider than the viewport", async () => {
    window.innerWidth = PHONE_WIDTH;
    window.dispatchEvent(new Event("resize"));
    vi.mocked(fetchBuckets).mockResolvedValue(buckets);
    renderDashboard(vi.fn(), "/bucket/cost");
    await within(sideNav()).findByRole("link", { name: "Cost" });

    await userEvent.click(screen.getByRole("button", { name: "Open navigation" }));

    expect(classesFixedWiderThanPhone(document.body)).toEqual([]);
    expect(document.body.querySelector("[style*='width']")).toBeNull();
    const stylesheet = await readStylesheet();
    expect(stylesheet).toContain(".login-card");
    expect(widthRulesTooWideForPhone(stylesheet)).toEqual([]);
  });
});
