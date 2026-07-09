"use client";
import { PageHeader } from "@/components/page-header";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export default function SettingsPage() {
  return (
    <div>
      <PageHeader title="Settings" description="Dashboard and connection configuration." />
      <Card>
        <CardHeader><CardTitle className="text-sm">Backend API</CardTitle></CardHeader>
        <CardContent>
          <code className="font-data text-sm">{process.env.NEXT_PUBLIC_API_URL ?? "(not set)"}</code>
          <p className="mt-2 text-xs text-muted">Set via NEXT_PUBLIC_API_URL. This is the only backend the dashboard talks to.</p>
        </CardContent>
      </Card>
    </div>
  );
}
