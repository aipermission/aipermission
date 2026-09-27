import { captureDatabaseConsolePresentation } from "../_shared/database-console-model";
import * as model from "./model";

export const postgresConsoleModel = captureDatabaseConsolePresentation("postgres", model);
