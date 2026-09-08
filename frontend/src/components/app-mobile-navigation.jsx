import { Menu } from "lucide-react";
import { useState } from "react";
import { AppSidebar } from "./app-sidebar";
import { Button } from "./ui/button";
import { Drawer } from "./ui/drawer";

export function AppMobileNavigation({ sidebarProps }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <header className="sticky top-0 z-30 flex h-14 items-center justify-between border-b border-stone-200 bg-white px-4 lg:hidden">
        <span className="inline-flex min-w-0 items-center gap-2 text-sm font-semibold">
          <img src="/icon.svg" alt="" className="h-8 w-8 rounded-md" />
          <span className="truncate">aipermission</span>
        </span>
        <Button type="button" variant="outline" className="h-9 w-9 px-0" aria-label="Open navigation" onClick={() => setOpen(true)}>
          <Menu className="h-4 w-4" />
        </Button>
      </header>
      <Drawer
        open={open}
        title="Navigation"
        description="AIPermission workspace"
        onClose={() => setOpen(false)}
        className="left-0 right-auto max-w-80 border-l-0 border-r"
        bodyClassName="p-0"
      >
        <AppSidebar {...sidebarProps} embedded onNavigate={() => setOpen(false)} />
      </Drawer>
    </>
  );
}
