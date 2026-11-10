package wistia

type ProgressFunc func(transferred, total int64)

type UploadResult struct {
	HashedID  string `json:"hashed_id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	FolderID  string `json:"project_id"`
	PlayerURL string `json:"player_url"`
	EmbedURL  string `json:"embed_url"`
}

type MediaInfo struct {
	HashedID string
	Name     string
	Status   string
}
