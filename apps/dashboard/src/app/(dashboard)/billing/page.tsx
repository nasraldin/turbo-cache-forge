import { PageHeader } from "@/components/page-header";

export default function BillingPage() {
  return (
    <div>
      <PageHeader title="Billing" description="Not available in the self-hosted edition." />
      <p className="rounded-md border border-dashed border-border p-8 text-center text-sm text-muted">
        Billing is a stub. Plans and metering land with the SaaS phase.
      </p>
    </div>
  );
}
