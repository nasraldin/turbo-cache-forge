FROM node:20-slim AS base
RUN corepack enable
WORKDIR /repo

FROM base AS deps
COPY pnpm-lock.yaml pnpm-workspace.yaml package.json .npmrc ./
COPY packages ./packages
COPY apps/dashboard/package.json ./apps/dashboard/package.json
RUN pnpm install --frozen-lockfile

FROM base AS build
COPY --from=deps /repo /repo
COPY packages ./packages
COPY apps/dashboard ./apps/dashboard
# NEXT_PUBLIC_* is inlined at build time — pass both as build args.
ARG NEXT_PUBLIC_API_URL=http://localhost:8080
# valid-FORMAT throwaway so the build's Clerk key-format check passes; override with the real (public) key.
ARG NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY=pk_test_ZXhhbXBsZS5jbGVyay5hY2NvdW50cy5kZXYk
# false (default) = personal mode: hide <OrganizationSwitcher/> (Clerk orgs disabled).
ARG NEXT_PUBLIC_ORG_ENABLED=false
ENV NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL
ENV NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY=$NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY
ENV NEXT_PUBLIC_ORG_ENABLED=$NEXT_PUBLIC_ORG_ENABLED
RUN pnpm --filter dashboard build

FROM node:20-slim AS run
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /repo/apps/dashboard/.next/standalone ./
COPY --from=build /repo/apps/dashboard/.next/static ./apps/dashboard/.next/static
COPY --from=build /repo/apps/dashboard/public ./apps/dashboard/public
EXPOSE 3000
CMD ["node", "apps/dashboard/server.js"]

# ponytail: NEXT_PUBLIC_* baked at build time by Next. For a self-hoster who wants to swap
# the API URL or Clerk key without rebuilding, the upgrade path is a small entrypoint that
# rewrites the built placeholder. Not built until someone needs it.
