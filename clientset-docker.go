package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/gin-gonic/gin"

	dTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

type RetrieveImagesRequest struct {
	Region         string `json:"region"`
	RepositoryName string `json:"repositoryName"`
}

type RetagImagesRequest struct {
	Region         string `json:"region"`
	RepositoryName string `json:"repositoryName"`
	NewLatestTag   string `json:"newLatestTag"`
	AccountID      string `json:"accountID"`
}

type Images struct {
	ImageDigest string `json:"imageDigest"`
	ImageTag    string `json:"imageTag"`
}

type ImageIds struct {
	ImageIds []Images `json:"imageIds"`
}

type Repository struct {
	RepositoryName string `json:"repositoryName"`
}

type Repositories struct {
	Repositories []Repository `json:"repositories"`
}

func newAWSConfig(region string) (aws.Config, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load aws config: %w", err)
	}
	return cfg, err
}

func getLoginPassword(ctx context.Context, ecr *ecr.Client) (string, error) {

	tkn, err := ecr.GetAuthorizationToken(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve ecr token: %w", err)
	}

	if len(tkn.AuthorizationData) == 0 {
		return "", fmt.Errorf("ecr token is empty")
	}
	if len(tkn.AuthorizationData) > 1 {
		return "", fmt.Errorf("multiple ecr tokens: length: %d", len(tkn.AuthorizationData))
	}
	if tkn.AuthorizationData[0].AuthorizationToken == nil {
		return "", fmt.Errorf("ecr token is nil")
	}

	str := *tkn.AuthorizationData[0].AuthorizationToken

	dec, err := base64.URLEncoding.DecodeString(str)
	if err != nil {
		return "", fmt.Errorf("failed to decode ecr token: %w", err)
	}

	spl := strings.Split(string(dec), ":")
	if len(spl) != 2 {
		return "", fmt.Errorf("unexpected ecr token format")
	}

	return spl[1], nil
}

func newDockerClient() (*client.Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}
	return cli, nil
}

func pullImage(ctx context.Context, cli *client.Client, img string, auth dTypes.AuthConfig) error {
	authData, err := json.Marshal(auth)
	if err != nil {
		return err
	}

	auths := base64.URLEncoding.EncodeToString(authData)

	out, err := cli.ImagePull(
		ctx,
		img,
		dTypes.ImagePullOptions{
			RegistryAuth: auths,
		})

	fmt.Println(out)
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(os.Stdout, out)
	if err != nil {
		return fmt.Errorf("failed to read image logs: %w", err)
	}
	return nil
}

func retagImages(c *gin.Context) {

	var requestBody RetagImagesRequest

	if err := c.BindJSON(&requestBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	region := requestBody.Region
	repositoryName := requestBody.RepositoryName
	newLatestTag := requestBody.NewLatestTag

	img := fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/%s:%s", requestBody.AccountID, region, repositoryName, newLatestTag)
	fmt.Println(img)
	ctx := context.TODO()

	cfg, err := newAWSConfig(region)
	if err != nil {
		panic(err)
	}

	ecr := ecr.NewFromConfig(cfg)

	pwd, err := getLoginPassword(ctx, ecr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}

	a := dTypes.AuthConfig{
		Username: "AWS",
		Password: pwd,
	}

	dkr, err := newDockerClient()
	if err != nil {
		panic(err)
	}

	err = pullImage(ctx, dkr, img, a)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
}

func getRepositories(c *gin.Context) {

	region := c.Param("region")

	cfg, err := newAWSConfig(region)
	if err != nil {
		panic(err)
	}

	slog.Debug("Creating ECR client")

	ecrClient := ecr.NewFromConfig(cfg)

	input := &ecr.DescribeRepositoriesInput{
		MaxResults: aws.Int32(10),
	}
	resp, err := ecrClient.DescribeRepositories(context.Background(), input)

	var repositories Repositories

	for _, repository := range resp.Repositories {
		repositories.Repositories = append(repositories.Repositories, Repository{
			RepositoryName: aws.ToString(repository.RepositoryName),
		})
	}

	for resp.NextToken != nil {
		input.NextToken = resp.NextToken
		resp, err = ecrClient.DescribeRepositories(context.Background(), input)
		for _, repository := range resp.Repositories {
			repositories.Repositories = append(repositories.Repositories, Repository{
				RepositoryName: aws.ToString(repository.RepositoryName),
			})
		}
	}
	if err != nil {
		slog.Error("Error listing images", err)
	}

	sort.SliceStable(repositories.Repositories, func(i, j int) bool {
		return repositories.Repositories[i].RepositoryName < repositories.Repositories[j].RepositoryName
	})

	c.JSON(http.StatusOK, repositories)
}

func RetrieveEcrContainers(c *gin.Context) {

	var images ImageIds
	var requestBody RetrieveImagesRequest

	if err := c.BindJSON(&requestBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	region := requestBody.Region
	repositoryName := requestBody.RepositoryName

	cfg, err := newAWSConfig(region)
	if err != nil {
		panic(err)
	}

	slog.Debug("Creating ECR client")

	ecrClient := ecr.NewFromConfig(cfg)

	input := &ecr.ListImagesInput{
		RepositoryName: aws.String(repositoryName),
		MaxResults:     aws.Int32(10),
		Filter: &types.ListImagesFilter{
			TagStatus: types.TagStatusTagged,
		},
	}
	resp, err := ecrClient.ListImages(context.Background(), input)
	if err != nil {
		c.JSON(http.StatusNotFound, "Registry not found")
		return
	}

	for _, imageID := range resp.ImageIds {
		if strings.Contains(*imageID.ImageTag, "1.0.0") {
			images.ImageIds = append(images.ImageIds, Images{
				ImageDigest: aws.ToString(imageID.ImageDigest),
				ImageTag:    aws.ToString(imageID.ImageTag),
			})
		}
	}
	for resp.NextToken != nil {
		input.NextToken = resp.NextToken
		resp, err = ecrClient.ListImages(context.Background(), input)
		for _, imageID := range resp.ImageIds {
			if strings.Contains(*imageID.ImageTag, "1.0.0") {
				images.ImageIds = append(images.ImageIds, Images{
					ImageDigest: aws.ToString(imageID.ImageDigest),
					ImageTag:    aws.ToString(imageID.ImageTag),
				})
			}
		}
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, "Error listing images")
		return
	}

	sort.SliceStable(images.ImageIds, func(i, j int) bool {
		return images.ImageIds[i].ImageTag > images.ImageIds[j].ImageTag
	})

	c.JSON(http.StatusOK, resp)

}

func Test(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("OK"))
}

func setupRouter() *gin.Engine {

	r := gin.Default()

	// Ping test
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	// Get user value
	r.POST("/repo/images", RetrieveEcrContainers)
	r.GET("/repo/:region", getRepositories)
	r.POST("/repo/retag", retagImages)

	return r
}

func main() {
	r := setupRouter()
	r.Run(":3333")
}
