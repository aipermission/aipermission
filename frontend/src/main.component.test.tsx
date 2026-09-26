import { beforeEach, describe, expect, it, vi } from "vitest";

const root = vi.hoisted(() => ({ create: vi.fn(), render: vi.fn() }));
vi.mock("react-dom/client", () => ({ createRoot: root.create }));
vi.mock("./App.jsx", () => ({ default: () => null }));

describe("frontend entrypoint", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    root.create.mockReturnValue({ render: root.render });
  });

  it("mounts the application into the explicit root element", async () => {
    const element = document.createElement("div");
    element.id = "root";
    document.body.append(element);
    await import("./main");
    expect(root.create).toHaveBeenCalledWith(element);
    expect(root.render).toHaveBeenCalledWith(expect.objectContaining({ type: expect.any(Function) }));
  });

  it("fails clearly instead of mounting elsewhere when the root is missing", async () => {
    await expect(import("./main")).rejects.toThrow("Missing application root");
    expect(root.create).not.toHaveBeenCalled();
    expect(root.render).not.toHaveBeenCalled();
  });
});
