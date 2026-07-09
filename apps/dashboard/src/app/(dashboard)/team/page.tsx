import { OrganizationProfile } from "@clerk/nextjs";
import { PageHeader } from "@/components/page-header";

// ponytail: Clerk already renders invite/role/remove UI — reimplementing it over our API
// would duplicate the IdP. Org membership stays in Clerk; our API only ever sees the org
// claim in the JWT.
export default function TeamPage() {
  return (
    <div>
      <PageHeader title="Team Members" description="Managed through your identity provider." />
      <OrganizationProfile routing="hash" />
    </div>
  );
}
