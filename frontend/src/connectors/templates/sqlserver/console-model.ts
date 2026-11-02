import { captureDatabaseConsolePresentation } from "../_shared/database-console-model";
import * as model from "./model";

export const sqlServerConsoleModel = captureDatabaseConsolePresentation("sqlserver", model);
