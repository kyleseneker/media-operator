package pvr

func hasTagLabels(opts Options) bool {
	for _, item := range opts.DownloadClients {
		if len(item.TagLabels) > 0 {
			return true
		}
	}
	for _, item := range opts.Indexers {
		if len(item.TagLabels) > 0 {
			return true
		}
	}
	for _, item := range opts.Notifications {
		if len(item.TagLabels) > 0 {
			return true
		}
	}
	for _, item := range opts.ImportLists {
		if len(item.TagLabels) > 0 {
			return true
		}
	}
	return false
}
