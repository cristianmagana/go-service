package dockerutils

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

func NewDockerClient() (*client.Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		slog.Error("Error creating docker client", err)
		return nil, err
	}
	return cli, nil
}

func NewAWSConfig(region string) (aws.Config, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
	if err != nil {
		slog.Error("Failed to load aws config: %w", err)
		return aws.Config{}, err
	}
	return cfg, err
}

func GetEcrClient(region string) *ecr.Client {

	cfg, err := NewAWSConfig(region)
	if err != nil {
		panic(err)
	}

	slog.Debug("Creating ECR client")

	return ecr.NewFromConfig(cfg)
}

func GetEcrLoginPassword(ecr *ecr.Client) (string, error) {

	tkn, err := ecr.GetAuthorizationToken(context.TODO(), nil)
	if err != nil {
		slog.Error("failed to retrieve ecr token: ", err)
		return "", err
	}

	if len(tkn.AuthorizationData) == 0 {
		slog.Error("ECR token is empty: ", err)
		return "", err
	}
	if len(tkn.AuthorizationData) > 1 {
		slog.Error("Multiple ECR tokens: length: %d", len(tkn.AuthorizationData))
		return "", err
	}
	if tkn.AuthorizationData[0].AuthorizationToken == nil {
		slog.Error("ECR token is nil")
		return "", err
	}

	str := *tkn.AuthorizationData[0].AuthorizationToken

	dec, err := base64.URLEncoding.DecodeString(str)
	if err != nil {
		slog.Error("Failed to decode ECR token: %w", err)
		return "", err
	}

	spl := strings.Split(string(dec), ":")
	if len(spl) != 2 {
		slog.Error("Unexpected ECR token format")
		return "", err
	}

	return spl[1], nil
}

func GetEcrRepositories(region string) ECRResponse {

	ecrClient := GetEcrClient(region)

	input := &ecr.DescribeRepositoriesInput{
		MaxResults: aws.Int32(10),
	}
	resp, err := ecrClient.DescribeRepositories(context.Background(), input)

	repos := ECRResponse{}

	for _, repository := range resp.Repositories {
		repos.Repositories = append(repos.Repositories, ECRRepository{
			RepositoryName: aws.ToString(repository.RepositoryName),
		})
	}

	for resp.NextToken != nil {
		input.NextToken = resp.NextToken
		resp, err = ecrClient.DescribeRepositories(context.Background(), input)
		for _, repository := range resp.Repositories {
			repos.Repositories = append(repos.Repositories, ECRRepository{
				RepositoryName: aws.ToString(repository.RepositoryName),
			})
		}
	}
	if err != nil {
		slog.Error("Error listing images", err)
	}

	sort.SliceStable(repos.Repositories, func(i, j int) bool {
		return repos.Repositories[i].RepositoryName < repos.Repositories[j].RepositoryName
	})

	return repos
}

func GetEcrContainers(ecrRequest *RetrieveImagesRequest) (DockerImagesResponse, error) {

	var images DockerImagesResponse

	cfg, err := NewAWSConfig(ecrRequest.Region)
	if err != nil {
		panic(err)
	}

	slog.Debug("Creating ECR client")

	ecrClient := ecr.NewFromConfig(cfg)

	slog.Debug("Listing images from ECR")

	input := &ecr.ListImagesInput{
		RepositoryName: aws.String(ecrRequest.RepositoryName),
		MaxResults:     aws.Int32(10),
		Filter: &types.ListImagesFilter{
			TagStatus: types.TagStatusTagged,
		},
	}
	resp, err := ecrClient.ListImages(context.Background(), input)
	if err != nil {
		slog.Error("Registry not found")
		return images, err
	}

	for _, imageID := range resp.ImageIds {
		if strings.Contains(*imageID.ImageTag, "1.0.0") {
			images.ImageIds = append(images.ImageIds, DockerImage{
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
				images.ImageIds = append(images.ImageIds, DockerImage{
					ImageDigest: aws.ToString(imageID.ImageDigest),
					ImageTag:    aws.ToString(imageID.ImageTag),
				})
			}
		}
	}
	if err != nil {
		slog.Error("Error listing images")
		return images, err
	}

	sort.SliceStable(images.ImageIds, func(i, j int) bool {
		return images.ImageIds[i].ImageTag > images.ImageIds[j].ImageTag
	})

	return images, nil
}

func PullImage(dockerClient *client.Client, auth dockerTypes.AuthConfig, pullImage *ImageRequest) error {

	img := fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/%s:%s", pullImage.AccountID, pullImage.Region, pullImage.RepositoryName, pullImage.Tag)

	// Get docker auth
	authData, err := json.Marshal(auth)
	if err != nil {
		return err
	}

	auths := base64.URLEncoding.EncodeToString(authData)

	// Pull image
	resp, err := dockerClient.ImagePull(
		context.TODO(),
		img,
		dockerTypes.ImagePullOptions{
			RegistryAuth: auths,
		})

	if err != nil {
		slog.Error("failed to pull image: ", err)
		return err
	}
	defer resp.Close()

	// Print the pull output
	_, err = io.Copy(os.Stdout, resp)
	if err != nil {
		slog.Error("failed to read image logs: ", err)
		return err
	}
	return nil
}

func RetagAndPushImage(dockerClient *client.Client, auth dockerTypes.AuthConfig, retagImage *ImageRequest) error {

	img := fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/%s:%s", retagImage.AccountID, retagImage.Region, retagImage.RepositoryName, retagImage.Tag)
	latest := fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/%s:%s", retagImage.AccountID, retagImage.Region, retagImage.RepositoryName, "latest")

	// Tag the image as "latest"
	err := dockerClient.ImageTag(context.Background(), img, latest)
	if err != nil {
		return err
	}

	// Get docker auth
	authData, err := json.Marshal(auth)
	if err != nil {
		return err
	}
	auths := base64.URLEncoding.EncodeToString(authData)

	// Push the tagged image
	res, err := dockerClient.ImagePush(context.TODO(), latest, dockerTypes.ImagePushOptions{
		RegistryAuth: auths,
	})
	if err != nil {
		return err
	}
	defer res.Close()

	// Print the push output
	_, err = io.Copy(os.Stdout, res)
	if err != nil {
		return err
	}

	slog.Info("Successfully pushed image: ", res)

	return nil
}
