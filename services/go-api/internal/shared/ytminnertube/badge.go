package ytminnertube

const explicitBadgeIcon = "MUSIC_EXPLICIT_BADGE"

func HasExplicitBadge(badges any) bool {
	list, _ := badges.([]any)
	for _, badge := range list {
		object, _ := badge.(map[string]any)
		renderer, _ := object["musicInlineBadgeRenderer"].(map[string]any)
		icon, _ := renderer["icon"].(map[string]any)
		if iconType, _ := icon["iconType"].(string); iconType == explicitBadgeIcon {
			return true
		}
	}
	return false
}
