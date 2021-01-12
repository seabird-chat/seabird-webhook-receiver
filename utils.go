package seabird_webhook_receiver

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if needle == item {
			return true
		}
	}
	return false
}
