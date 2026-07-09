package turbo

// StorageKey namespaces artifacts by org so tenants never collide.
func StorageKey(orgSlug, hash string) string { return orgSlug + "/" + hash }
