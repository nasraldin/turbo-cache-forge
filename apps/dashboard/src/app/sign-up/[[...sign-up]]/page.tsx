"use client";
import { SignUp } from "@clerk/nextjs";
import { redirect } from "next/navigation";
import { useSession } from "@/app/session";

// Built-in auth has no self-registration — there is one root user.
export default function Page() {
  const { mode } = useSession();
  if (mode === "builtin") redirect("/sign-in");
  return (
    <div className="grid min-h-screen place-items-center bg-bg">
      <SignUp />
    </div>
  );
}
