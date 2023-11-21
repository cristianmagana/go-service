package dockerutils

type ECRRepository struct {
	RepositoryName string `json:"repositoryName"`
	// Add more fields here if needed
}

type ECRResponse struct {
	Repositories []ECRRepository `json:"repositories"`
	// Add more fields here if needed
}

type DockerImage struct {
	ImageDigest string `json:"imageDigest"`
	ImageTag    string `json:"imageTag"`
}

type DockerImagesResponse struct {
	ImageIds []DockerImage `json:"imageIds"`
}

type RetrieveImagesRequest struct {
	Region         string `json:"region"`
	RepositoryName string `json:"repositoryName"`
}

type ImageRequest struct {
	Region         string `json:"region"`
	RepositoryName string `json:"repositoryName"`
	Tag            string `json:"newLatestTag"`
	AccountID      string `json:"accountID"`
}
