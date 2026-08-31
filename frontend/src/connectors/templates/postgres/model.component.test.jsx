import { describe, expect, it } from "vitest";
import { credentialDeleteDialog } from "./model";

describe("Postgres credential deletion", () => {
  it("explains managed-role ownership reassignment", () => {
    const dialog = credentialDeleteDialog({
      row: {
        name: "reader",
        target_id: 4,
        target_label: "main-db",
        profile: {
          public: {
            managed_by_aipermission: true,
            managed_role_name: "app_reader",
            managed_admin_profile_id: 8,
          },
        },
      },
      targets: [
        {
          id: 4,
          profiles: [{ id: 8, public: { username: "postgres_admin" } }],
        },
      ],
    });

    expect(dialog.details.find((item) => item.label === "Managed role")?.value).toBe("app_reader");
    expect(dialog.details.find((item) => item.label === "Ownership target")?.value).toBe("postgres_admin");
    expect(dialog.notice).toMatch(/reassigned to postgres_admin/);
    expect(dialog.notice).toMatch(/Privileges owned by app_reader will be removed/);
  });

  it("leaves ordinary Postgres credentials on the shared confirmation", () => {
    expect(credentialDeleteDialog({ row: { profile: { public: { managed_by_aipermission: false } } } })).toBeNull();
  });
});
