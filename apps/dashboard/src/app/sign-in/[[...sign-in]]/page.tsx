import { SignIn } from "@clerk/nextjs";

export default function Page() {
  return (
    <div className="grid min-h-screen place-items-center bg-bg">
      <SignIn />
    </div>
  );
}
