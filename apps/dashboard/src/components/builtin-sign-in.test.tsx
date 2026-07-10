import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const login = vi.fn();
vi.mock("@/app/session", () => ({ useSession: () => ({ mode: "builtin", login }) }));
const replace = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace }) }));

import { BuiltinSignIn } from "./builtin-sign-in";

afterEach(() => {
  login.mockReset();
  replace.mockReset();
});

function submit() {
  fireEvent.change(screen.getByLabelText(/username/i), { target: { value: "root" } });
  fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "hunter2" } });
  fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
}

describe("BuiltinSignIn", () => {
  it("logs in and redirects to / on success", async () => {
    login.mockResolvedValueOnce(undefined);
    render(<BuiltinSignIn />);
    submit();
    await waitFor(() => expect(login).toHaveBeenCalledWith("root", "hunter2"));
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/"));
  });

  it("shows an inline error on failure", async () => {
    login.mockRejectedValueOnce(new Error("Invalid username or password"));
    render(<BuiltinSignIn />);
    submit();
    expect(await screen.findByText(/invalid username or password/i)).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
  });
});
