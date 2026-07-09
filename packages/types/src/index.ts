// Shapes for /api/v1, mirrored 1:1 from the actual Phase-3 Go handlers
// (services/api/internal/mgmt/handlers.go + internal/db/repo.go), not from
// the OpenAPI doc (which has no response schemas) or invented conventions.
//
// ponytail: casing used to be inconsistent (Project/Artifact serialized
// PascalCase because db.Project/db.Artifact had no json struct tags, while
// Token/Stats were hand-built snake_case maps). Fixed by adding json tags in
// services/api/internal/db/repo.go — the whole /api/v1 surface is now
// snake_case, so these types no longer need to document the wart.

// GET /api/v1/stats
export interface Stats {
  storage_bytes: number;
  artifact_count: number;
  hits: number;
  misses: number;
  requests: number;
  bytes_up: number;
  bytes_down: number;
}

// GET /api/v1/projects (array) and 201 body of POST /api/v1/projects.
export interface Project {
  id: number;
  slug: string;
  name: string;
  created_at: string;
}

// Element of the `artifacts` array in GET /api/v1/artifacts.
// No project-slug field exists on the backend struct/query — do not invent one.
export interface Artifact {
  hash: string;
  size_bytes: number;
  tag: string | null;
  created_at: string;
  last_accessed_at: string;
}

// GET /api/v1/artifacts?limit=&offset= — offset pagination, not cursor-based.
export interface ArtifactsPage {
  limit: number;
  offset: number;
  artifacts: Artifact[];
}

// GET /api/v1/tokens (array). Hand-built snake_case map; never includes the
// token secret.
export interface Token {
  id: number;
  name: string;
  created_at: string;
  last_used_at: string | null;
  revoked_at: string | null;
}

// 201 body of POST /api/v1/tokens — plaintext present exactly once, on
// create. The backend only echoes id/name/token here (no timestamps), so
// this does NOT extend Token.
export interface CreatedToken {
  id: number;
  name: string;
  token: string;
}
