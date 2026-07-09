"use client";
import { SignIn } from "@clerk/nextjs";
import { useSession } from "@/app/session";
import { BuiltinSignIn } from "@/components/builtin-sign-in";

export default function Page() {
  const { mode } = useSession();
  if (mode === "builtin") return <BuiltinSignIn />;
  return (
    <div className="grid min-h-screen place-items-center bg-bg">
      <SignIn />
    </div>
  );
}
