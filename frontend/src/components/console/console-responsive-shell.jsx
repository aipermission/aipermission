import { cloneElement, useState } from "react";
import { Database, TicketCheck } from "lucide-react";
import { useMediaQuery } from "../../lib/use-media-query";
import { Button } from "../ui/button";
import { Drawer } from "../ui/drawer";
import { consoleShellGridClass } from "./console-layout";

export function ConsoleResponsiveShell({ targetsCompact, tokensCompact, targetSidebar, workspace, tokenPanel, dialogs }) {
  const [targetsDrawerOpen, setTargetsDrawerOpen] = useState(false);
  const [tokensDrawerOpen, setTokensDrawerOpen] = useState(false);
  const wide = useMediaQuery("(min-width: 1536px)");
  const targets = cloneElement(targetSidebar, {
    compact: wide && targetsCompact,
    onSelect: (...args) => {
      targetSidebar.props.onSelect?.(...args);
      setTargetsDrawerOpen(false);
    },
  });
  const tokens = cloneElement(tokenPanel, { compact: wide && tokensCompact });

  return (
    <section
      className={`grid h-[calc(100dvh-116px)] min-h-[480px] grid-cols-[minmax(0,1fr)] grid-rows-[auto_minmax(0,1fr)] gap-3 lg:h-[calc(100vh-40px)] lg:min-h-[640px] 2xl:grid-rows-1 2xl:gap-4 ${consoleShellGridClass(targetsCompact, tokensCompact)}`}
    >
      {wide ? (
        targets
      ) : (
        <div className="flex items-center justify-between gap-2 2xl:hidden">
          <Button type="button" variant="outline" className="min-w-0 flex-1" onClick={() => setTargetsDrawerOpen(true)}>
            <Database className="h-4 w-4" />
            Connectors
          </Button>
          <Button type="button" variant="outline" className="min-w-0 flex-1" onClick={() => setTokensDrawerOpen(true)}>
            <TicketCheck className="h-4 w-4" />
            Tokens
          </Button>
        </div>
      )}
      {workspace}
      {wide ? tokens : null}
      <Drawer
        open={!wide && targetsDrawerOpen}
        title="Connectors"
        onClose={() => setTargetsDrawerOpen(false)}
        className="left-0 right-auto max-w-sm border-l-0 border-r"
        bodyClassName="p-3"
      >
        {targets}
      </Drawer>
      <Drawer
        open={!wide && tokensDrawerOpen}
        title="Tokens"
        onClose={() => setTokensDrawerOpen(false)}
        className="max-w-sm"
        bodyClassName="p-3"
      >
        {tokens}
      </Drawer>
      {dialogs}
    </section>
  );
}
