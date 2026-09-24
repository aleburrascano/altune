import type { ReactNode } from "react";
import * as Tabs from "@radix-ui/react-tabs";
import { focusRing } from "./focusRing";

export interface TabSection {
  title: string;
  content: ReactNode;
}

export function SectionTabs({ label, sections }: { label: string; sections: [TabSection, ...TabSection[]] }) {
  return (
    <Tabs.Root defaultValue={sections[0].title} className="flex min-w-0 flex-col gap-3">
      <Tabs.List aria-label={label} className="flex gap-1 overflow-x-auto border-b border-border">
        {sections.map((section) => (
          <Tabs.Trigger
            key={section.title}
            value={section.title}
            className={`-mb-px border-0 border-b-2 border-transparent bg-transparent px-3 py-1.5 text-sm text-fg-dim hover:text-fg data-[state=active]:border-accent data-[state=active]:text-fg ${focusRing}`}
          >
            {section.title}
          </Tabs.Trigger>
        ))}
      </Tabs.List>
      {sections.map((section) => (
        <Tabs.Content key={section.title} value={section.title} className={`min-w-0 ${focusRing}`}>
          {section.content}
        </Tabs.Content>
      ))}
    </Tabs.Root>
  );
}
