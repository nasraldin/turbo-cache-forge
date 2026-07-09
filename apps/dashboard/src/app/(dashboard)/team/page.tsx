import { OrganizationProfile } from "@clerk/nextjs";
import { PageHeader } from "@/components/page-header";

// ponytail: Clerk already renders invite/role/remove UI — reimplementing it over our API
// would duplicate the IdP. Org membership stays in Clerk; our API only ever sees the org
// claim in the JWT.
const orgEnabled = process.env.NEXT_PUBLIC_ORG_ENABLED === "true";

export default function TeamPage() {
  return (
    <div>
      <PageHeader title="Team Members" description="Managed through your identity provider." />
      {orgEnabled ? (
        <OrganizationProfile routing="hash" />
      ) : (
        <p className="rounded-md border border-dashed border-border p-8 text-center text-sm text-muted">
          Running in personal mode — each account is its own workspace. Enable Clerk organizations
          (and set <code className="font-data">NEXT_PUBLIC_ORG_ENABLED=true</code>) to invite and
          manage teammates.
        </p>
      )}
    </div>
  );
}
