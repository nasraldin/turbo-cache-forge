// Shapes for /api/v1, mirrored 1:1 from the actual Phase-3 Go handlers
// (services/api/internal/mgmt/handlers.go + internal/db/repo.go), not from
// the OpenAPI doc (which has no response schemas) or invented conventions.
//
// ponytail: casing is inconsistent across endpoints in the real API —
// Project/Artifact have no `json:` struct tags in Go, so encoding/json
// serializes their exported Go field names verbatim (PascalCase); Token and
// Stats are hand-built with map[string]any using snake_case keys. This file
// documents that wart rather than "fixing" it — fixing it means adding json
// tags to services/api/internal/db, which is out of scope for this task
// (Go backend stays untouched). Flag as a Phase-3 follow-up if consistency
// is wanted later.

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
// PascalCase because db.Project has no json struct tags.
export interface Project {
  ID: number;
  Slug: string;
  Name: string;
  CreatedAt: string;
}

// Element of the `artifacts` array in GET /api/v1/artifacts.
// PascalCase because db.Artifact has no json struct tags. No project-slug
// field exists on the backend struct/query — do not invent one.
export interface Artifact {
  Hash: string;
  SizeBytes: number;
  Tag: string | null;
  CreatedAt: string;
  LastAccessedAt: string;
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
