import type { connectorConsoleTheme } from "./console-theme";
import type { SQLConsoleController } from "./use-sql-console";

export type SQLConsoleViewProps = {
  controller: SQLConsoleController;
  styles: ReturnType<typeof connectorConsoleTheme>;
  theme: "dark" | "light";
};
