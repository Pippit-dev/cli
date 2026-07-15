package common

// StringSet builds a membership set from a string list.
func StringSet(list []string) map[string]struct{} {
	set := make(map[string]struct{}, len(list))
	for _, value := range list {
		set[value] = struct{}{}
	}
	return set
}
