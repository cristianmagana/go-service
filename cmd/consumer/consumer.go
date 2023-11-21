package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	dockerUtils "github.com/cristian/go-service/pkg/docker-utils"
	k8sUtils "github.com/cristianmagana/go-service/pkg/clientset"

	dockerTypes "github.com/docker/docker/api/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func listPods(clientSet *kubernetes.Clientset) {
	pods, err := clientSet.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		panic(err)
	}

	for _, pod := range pods.Items {
		fmt.Printf("Pod name: %s\n", pod.Name)
	}
}

func main() {
	clusterName := os.Getenv("CLUSTER_NAME")
	region := os.Getenv("AWS_REGION")
	repoName := os.Getenv("REPO_NAME")

	slog.Info("Getting clientset")
	clientSet := k8sUtils.GetEksClientset(clusterName, region)
	listPods(clientSet)

	client, err := dockerUtils.NewDockerClient()
	if err != nil {
		slog.Error(err.Error())
	}

	ecrRepos := dockerUtils.GetEcrRepositories(region)
	fmt.Printf("ECR Repos: %v\n", ecrRepos)

	getImagesRequest := &dockerUtils.RetrieveImagesRequest{Region: "us-east-1", RepositoryName: repoName}
	slog.Info("========================")

	ecrContainers, err := dockerUtils.GetEcrContainers(getImagesRequest)
	if err != nil {
		slog.Error(err.Error())
	}

	fmt.Printf("ECR Containers: %v\n", ecrContainers)
	slog.Info("========================")

	ecrClient := dockerUtils.GetEcrClient(region)
	slog.Info("========================")

	pwd, err := dockerUtils.GetEcrLoginPassword(ecrClient)
	if err != nil {
		slog.Error(err.Error())
	}

	slog.Info("========================")

	a := dockerTypes.AuthConfig{
		Username: "AWS",
		Password: pwd,
	}

	slog.Info("========================")
	pullImageRequest := &dockerUtils.ImageRequest{Region: "us-east-1", RepositoryName: repoName, Tag: "1.0.0-dev-2693", AccountID: "656715373819"}
	dockerUtils.PullImage(client, a, pullImageRequest)

	slog.Info("========================")

	retagImageRequest := &dockerUtils.ImageRequest{Region: "us-east-1", RepositoryName: repoName, Tag: "1.0.0-dev-2693", AccountID: "656715373819"}
	dockerUtils.RetagAndPushImage(client, a, retagImageRequest)

	slog.Info("========retag^============")
}
