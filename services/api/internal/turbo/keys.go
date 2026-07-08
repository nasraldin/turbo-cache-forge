package turbo

// storageKey namespaces artifacts by org so tenants never collide.
func storageKey(orgSlug, hash string) string { return orgSlug + "/" + hash }
