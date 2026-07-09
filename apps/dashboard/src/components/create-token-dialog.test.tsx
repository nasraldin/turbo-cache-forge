import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { CreateTokenDialog } from "./create-token-dialog";

describe("CreateTokenDialog", () => {
  it("creates a token and reveals the plaintext exactly once", async () => {
    // CreatedToken is exactly { id, name, token } — no timestamps.
    const createToken = vi.fn().mockResolvedValue({ id: 7, name: "ci", token: "turbo_ONE_TIME_SECRET" });
    const onCreated = vi.fn();
    render(<CreateTokenDialog createToken={createToken} onCreated={onCreated} />);

    await userEvent.click(screen.getByRole("button", { name: /new token/i }));
    await userEvent.type(screen.getByLabelText(/name/i), "ci");
    await userEvent.click(screen.getByRole("button", { name: /^create$/i }));

    expect(await screen.findByText("turbo_ONE_TIME_SECRET")).toBeInTheDocument();
    expect(screen.getByText(/won.t be able to see it again/i)).toBeInTheDocument();
    expect(createToken).toHaveBeenCalledWith({ name: "ci" });
    expect(onCreated).toHaveBeenCalled();

    // closing the dialog forgets the secret
    await userEvent.click(screen.getByRole("button", { name: /done/i }));
    expect(screen.queryByText("turbo_ONE_TIME_SECRET")).not.toBeInTheDocument();
  });
});
