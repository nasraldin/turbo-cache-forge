package turbo

// maxHashLen bounds artifact hash length; real Turbo hashes are short
// hex/base tokens, so this is generous headroom, not a real-world ceiling.
const maxHashLen = 512

// validHash reports whether hash is safe to use as a storage key segment.
// It only accepts non-empty, length-bounded [A-Za-z0-9_-] tokens — the same
// shape real Turbo artifact hashes take. This rejects '/', '.' (so ".." is
// impossible), backslashes, whitespace, and any other character that could
// let a hostile {hash} URL param escape its org's key prefix, independent of
// how any particular storage backend happens to interpret the key.
func validHash(hash string) bool {
	if hash == "" || len(hash) > maxHashLen {
		return false
	}
	for _, c := range hash {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}
