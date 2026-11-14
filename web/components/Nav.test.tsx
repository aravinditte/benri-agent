import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Nav from "./Nav";
import { LanguageProvider } from "@/lib/i18n";

const nav = vi.hoisted(() => ({ pathname: "/", push: vi.fn() }));

vi.mock("next/link", () => ({
  default: ({ href, children, ...props }: React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a href={String(href)} {...props}>{children}</a>
  ),
}));
vi.mock("next/navigation", () => ({
  usePathname: () => nav.pathname,
  useRouter: () => ({ push: nav.push }),
}));

describe("Nav", () => {
  beforeEach(() => {
    nav.pathname = "/";
    nav.push.mockReset();
    window.localStorage.clear();
    window.localStorage.setItem("benri_user", JSON.stringify({
      id: "user-operator",
      tenant_id: "tenant-hinata",
      email: "operator@hinata.example",
      role: "operator",
    }));
  });

  afterEach(cleanup);

  it("shows the signed-in operator and switches the navigation to English", async () => {
    render(<LanguageProvider><Nav /></LanguageProvider>);
    await waitFor(() => expect(screen.getByText(/operator@hinata\.example/)).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "EN" }));
    expect(screen.getByText("Queue")).toBeTruthy();
    expect(screen.getByText("Escalations")).toBeTruthy();
  });

  it("logs out and returns to the login screen", async () => {
    render(<LanguageProvider><Nav /></LanguageProvider>);
    fireEvent.click(screen.getByRole("button", { name: "EN" }));
    fireEvent.click(await screen.findByRole("button", { name: "Logout" }));
    expect(window.localStorage.getItem("benri_token")).toBeNull();
    expect(window.localStorage.getItem("benri_user")).toBeNull();
    expect(nav.push).toHaveBeenCalledWith("/login");
  });
});
