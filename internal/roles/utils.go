// utils.go contains small string helpers shared across the roles package.
package roles

func trim(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}
