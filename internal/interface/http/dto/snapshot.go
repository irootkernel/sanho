package dto

type GetSnapshotResponse struct {
	Commit   string `json:"commit"`
	Snapshot string `json:"snapshot"`
}
