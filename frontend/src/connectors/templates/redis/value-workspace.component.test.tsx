import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { RedisValueWorkspace, type RedisValueBrowser } from "./value-workspace";
import { connectorConsoleTheme } from "../_shared/console-theme";

function valueBrowser(overrides: Partial<RedisValueBrowser> = {}): RedisValueBrowser {
  return {
    activeKey: "",
    keyResult: null,
    product: "Redis",
    creatingKey: true,
    resultMode: "value",
    setResultMode: vi.fn(),
    loadKey: vi.fn(async () => {}),
    state: { state: "idle", error: "", message: "" },
    ttlDraft: "",
    setTTLDraft: vi.fn(),
    canUpdateTTL: false,
    updateTTL: vi.fn(),
    canSaveString: true,
    editableString: true,
    saveStringValue: vi.fn(),
    newKey: "",
    setNewKey: vi.fn(),
    newValue: "",
    setNewValue: vi.fn(),
    valueDraft: "",
    setValueDraft: vi.fn(),
    ...overrides,
  };
}

describe("RedisValueWorkspace", () => {
  it("renders truncated string values as a read-only bounded preview", () => {
    render(
      <RedisValueWorkspace
        styles={connectorConsoleTheme("light")}
        browser={valueBrowser({
          activeKey: "large-json",
          keyResult: {
            key: "large-json",
            type: "string",
            value: '{"id":9007199254740993}...[truncated]',
            ttl_ms: -1,
            truncated: true,
          },
          product: "Redis",
          creatingKey: false,
          resultMode: "value",
          setResultMode: vi.fn(),
          loadKey: vi.fn(),
          state: { state: "idle", error: "", message: "" },
          ttlDraft: "",
          setTTLDraft: vi.fn(),
          canUpdateTTL: true,
          updateTTL: vi.fn(),
          canSaveString: false,
          editableString: false,
          saveStringValue: vi.fn(),
        })}
      />,
    );

    expect(screen.getByText(/bounded preview is read-only/i)).toBeInTheDocument();
    expect(screen.getByText('{"id":9007199254740993}...[truncated]')).toBeInTheDocument();
    expect(screen.queryByRole("textbox", { name: "String value" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Read only" })).toBeDisabled();
  });

  it("routes editable new-key controls to the browser owner", async () => {
    const user = userEvent.setup();
    const browser = valueBrowser({
      activeKey: "",
      keyResult: null,
      product: "Redis",
      creatingKey: true,
      resultMode: "value",
      setResultMode: vi.fn(),
      state: { state: "idle", error: "", message: "" },
      newKey: "",
      setNewKey: vi.fn(),
      newValue: "",
      setNewValue: vi.fn(),
      ttlDraft: "",
      setTTLDraft: vi.fn(),
      canUpdateTTL: false,
      updateTTL: vi.fn(),
      canSaveString: true,
      editableString: true,
      saveStringValue: vi.fn(),
    });
    render(<RedisValueWorkspace styles={connectorConsoleTheme("light")} browser={browser} />);

    await user.type(screen.getByLabelText("New key name"), "cache:key");
    await user.type(screen.getByLabelText("String value"), "value");
    await user.click(screen.getByRole("button", { name: "Raw JSON" }));
    await user.click(screen.getByRole("button", { name: "Create key" }));

    expect(browser.setNewKey).toHaveBeenCalled();
    expect(browser.setNewValue).toHaveBeenCalled();
    expect(browser.setResultMode).toHaveBeenCalledWith("json");
    expect(browser.saveStringValue).toHaveBeenCalledOnce();
  });
});
