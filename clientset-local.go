package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go/aws"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

func CreateKubernetesClientSetLocal(clusterName string, region string, profile string) *kubernetes.Clientset {

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithSharedConfigProfile(*&profile),
	)
	if err != nil {
		panic(fmt.Sprintf("failed to load config, %v", err))
	}
	slog.Debug("Creating EKS client")

	eksClient := eks.NewFromConfig(cfg)

	slog.Debug(clusterName)

	cluster, err := eksClient.DescribeCluster(context.TODO(), &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	})
	if err != nil {
		panic(err)
	}
	if cluster.Cluster == nil {
		panic(fmt.Errorf("cluster %s not found", clusterName))
	}

	gen, err := token.NewGenerator(true, false)
	if err != nil {
		slog.Error("error creating token generator: %v\n", err)
	}

	opts := &token.GetTokenOptions{
		ClusterID: *cluster.Cluster.Name,
	}

	tok, err := gen.GetWithOptions(opts)
	if err != nil {
		slog.Error("error getting token: %v\n", err)
	}

	ca, err := base64.StdEncoding.DecodeString(*cluster.Cluster.CertificateAuthority.Data)
	if err != nil {
		slog.Error("error decoding certificate authority data: %v\n", err)
	}

	clientset, err := kubernetes.NewForConfig(
		&rest.Config{
			Host:        *cluster.Cluster.Endpoint,
			BearerToken: tok.Token,
			TLSClientConfig: rest.TLSClientConfig{
				CAData: ca,
			},
		},
	)

	if err != nil {
		slog.Error("error creating kubernetes client: %v\n", err)
		panic(err)
	}

	return clientset
}

func main() {

	clusterName := os.Getenv("CLUSTER_NAME")
	if clusterName == "" {
		slog.Error("CLUSTER_NAME environment variable not set")
		panic("CLUSTER_NAME environment variable not set")
	}
	region := os.Getenv("AWS_REGION")
	if region == "" {
		slog.Error("AWS_REGION environment variable not set")
		panic("AWS_REGION environment variable not set")
	}
	profile := os.Getenv("AWS_PROFILE")
	if profile == "" {
		slog.Error("AWS_PROFILE environment variable not set")
		panic("AWS_PROFILE environment variable not set")
	}

	clientSet := CreateKubernetesClientSetLocal(clusterName, region, profile)

	pods, err := clientSet.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		panic(err)
	}

	for _, pod := range pods.Items {
		fmt.Printf("Pod name: %s\n", pod.Name)
	}
}
